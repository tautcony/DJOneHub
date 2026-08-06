package linux

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/transport"
)

// Adapter uses the same read-only sysfs discovery shape as vohive-open. It
// deliberately does not open a control device during discovery: backend
// initialization owns protocol probing and the runtime still manages one
// candidate only.
type Adapter struct {
	*unsupported.Adapter
	sysfsRoot string
	// probeFail remembers tty ports that did not answer AT, so the poll-driven
	// discovery does not burn a serial timeout on them on every scan.
	probeMu   sync.Mutex
	probeFail map[string]time.Time
}

// atProbeTimeout, atProbeFailCooldown and atProbeBudget bound the cost of AT
// port probing. Opening a usb-serial option port takes seconds on Linux, so
// probing is only affordable once: a successful probe is served by the IMEI
// cache for 10 minutes, ports that failed are skipped for atProbeFailCooldown,
// and atProbeBudget stops a scan where nothing answers AT at all.
const (
	atProbeTimeout      = 2 * time.Second
	atProbeFailCooldown = 10 * time.Minute
	atProbeBudget       = 25 * time.Second
)

func New() *Adapter {
	return &Adapter{Adapter: unsupported.New("linux", device.CapabilitySet{
		device.CapabilityDeviceStatus:  "USB/sysfs modem discovery",
		device.CapabilityNetworkStatus: "Linux network interface status",
	}), sysfsRoot: "/sys/bus/usb/devices", probeFail: make(map[string]time.Time)}
}

func (a *Adapter) Discover(ctx context.Context) ([]device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates, err := discoverSysfs(ctx, a.sysfsRoot)
	if err != nil {
		return nil, err
	}
	// 单设备契约 (见 transport.DeviceDiscovery): 运行时只消费 candidates[0],
	// 因此只对将被消费的候选做 AT 探测, 不为其余候选耗费探测工作。
	if len(candidates) > 0 {
		a.selectATPort(ctx, &candidates[0])
	}
	return candidates, nil
}

// DetectEDL reports whether a Qualcomm emergency download device is present.
// EDL has no AT channel, so it must be detected directly from the USB tree.
func (a *Adapter) DetectEDL(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(a.sysfsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Linux USB sysfs: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		name := entry.Name()
		if strings.HasPrefix(name, "usb") {
			continue
		}
		path := filepath.Join(a.sysfsRoot, name)
		vid := readHex(filepath.Join(path, "idVendor"))
		pid := readHex(filepath.Join(path, "idProduct"))
		if vid == 0x05c6 && pid == 0x9008 {
			return true, nil
		}
	}
	return false, nil
}

func discoverSysfs(ctx context.Context, root string) ([]device.Candidate, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Linux USB sysfs: %w", err)
	}

	result := make([]device.Candidate, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if strings.HasPrefix(name, "usb") {
			continue
		}
		candidate, ok := discoverUSBDevice(filepath.Join(root, name))
		if !ok {
			continue
		}
		key := candidate.StableID()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].StableID() < result[j].StableID() })
	return result, nil
}

func discoverUSBDevice(path string) (device.Candidate, bool) {
	vid := readHex(filepath.Join(path, "idVendor"))
	pid := readHex(filepath.Join(path, "idProduct"))
	if vid == 0 || pid == 0 {
		return device.Candidate{}, false
	}
	interfaces, _ := filepath.Glob(filepath.Join(path, filepath.Base(path)+":1.*"))
	var atPorts []string
	var controlPath, networkInterface, driver string
	for _, intf := range interfaces {
		if value := readLinkBase(filepath.Join(intf, "driver")); value != "" {
			driver = value
		}
		for _, pattern := range []string{
			filepath.Join(intf, "tty", "ttyUSB*"),
			filepath.Join(intf, "tty", "ttyACM*"),
			filepath.Join(intf, "ttyUSB*"),
			filepath.Join(intf, "ttyACM*"),
		} {
			for _, tty := range mustGlob(pattern) {
				atPorts = append(atPorts, filepath.Join("/dev", filepath.Base(tty)))
			}
		}
		if controlPath == "" {
			for _, pattern := range []string{
				filepath.Join(intf, "usbmisc", "cdc-wdm*"),
				filepath.Join(intf, "cdc-wdm*"),
			} {
				matches := mustGlob(pattern)
				if len(matches) > 0 {
					controlPath = filepath.Join("/dev", filepath.Base(matches[0]))
					break
				}
			}
		}
		if networkInterface == "" {
			matches := mustGlob(filepath.Join(intf, "net", "*"))
			if len(matches) > 0 {
				networkInterface = filepath.Base(matches[0])
			}
		}
	}
	if len(atPorts) == 0 && controlPath == "" && networkInterface == "" {
		return device.Candidate{}, false
	}
	sort.Strings(atPorts)
	atPorts = unique(atPorts)
	mode := classifyMode(controlPath, driver)
	identity := device.Identity{
		StableID:         "linux/" + filepath.Base(path),
		PhysicalLocation: filepath.Base(path),
		VendorID:         fmt.Sprintf("%04x", vid),
		ProductID:        fmt.Sprintf("%04x", pid),
		Manufacturer:     usbVendor(vid),
		Product:          usbProduct(vid, pid),
	}
	metadata := map[string]string{"discovery": "linux-sysfs", "driver": driver, "mode": mode}
	if controlPath != "" {
		metadata["control_path"] = controlPath
	}
	if networkInterface != "" {
		metadata["network_interface"] = networkInterface
	}
	candidate := device.Candidate{Identity: identity, Metadata: metadata, ControlPath: controlPath, NetworkInterface: networkInterface}
	if len(atPorts) > 0 {
		candidate.ATPorts = atPorts
		candidate.ATPort = atPorts[0]
	}
	return candidate, true
}

// selectATPort probes the candidate tty ports for an AT response and keeps the
// responding port. Quectel dongles expose several tty ports (DM, NMEA, AT, PPP)
// and the first sorted port is not the AT one, so the sysfs default alone is
// not usable.
func (a *Adapter) selectATPort(ctx context.Context, c *device.Candidate) {
	if len(c.ATPorts) < 2 {
		return // single-port devices have no alternative to choose
	}
	start := time.Now()
	// Probe from the highest-numbered tty down: Quectel option-driver dongles
	// expose several tty ports (DM, NMEA, AT, PPP) and answer AT at or near the
	// last one, so the first probe usually finds the AT port.
	order := make([]string, len(c.ATPorts))
	for i, port := range c.ATPorts {
		order[len(c.ATPorts)-1-i] = port
	}
	chosen := chooseATPort(order, func(port string) (bool, bool) {
		if ctx.Err() != nil || time.Since(start) > atProbeBudget {
			return false, false
		}
		if a.probeFailedRecently(port) {
			return false, false
		}
		imei, err := modem.ProbeIMEICached(port, atProbeTimeout)
		if err == nil && imei != "" {
			return true, false
		}
		if isBusySerialErr(err) {
			return false, true
		}
		a.rememberProbeFailure(port)
		return false, false
	})
	if chosen != "" {
		c.ATPort = chosen
	}
}

// chooseATPort runs probe on each port in order and returns the first port
// that answers AT. When no port answers, a port held busy by another process
// (e.g. ModemManager) is returned as the best guess: an external process that
// grabbed it has already identified it as the AT port, and Manager.Start
// force-releases it before opening.
func chooseATPort(ports []string, probe func(port string) (ok, busy bool)) string {
	var busy string
	for _, port := range ports {
		ok, isBusy := probe(port)
		if ok {
			return port
		}
		if isBusy && busy == "" {
			busy = port
		}
	}
	return busy
}

func isBusySerialErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "serial port busy") ||
		strings.Contains(msg, "resource busy") ||
		strings.Contains(msg, "device or resource busy")
}

func (a *Adapter) probeFailedRecently(port string) bool {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	failAt, ok := a.probeFail[port]
	if !ok {
		return false
	}
	if time.Since(failAt) < atProbeFailCooldown {
		return true
	}
	delete(a.probeFail, port)
	return false
}

func (a *Adapter) rememberProbeFailure(port string) {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	a.probeFail[port] = time.Now()
}

func (a *Adapter) Status(ctx context.Context, candidate device.Candidate) (transport.NetworkStatus, error) {
	if err := ctx.Err(); err != nil {
		return transport.NetworkStatus{}, err
	}
	name := moduleNetworkInterface(candidate)
	if name == "" {
		return transport.NetworkStatus{}, errors.CapabilityMissing(string(device.CapabilityNetworkStatus), "network_status", "no modem network interface was discovered")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return transport.NetworkStatus{}, fmt.Errorf("read network interface %s: %w", name, err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return transport.NetworkStatus{}, fmt.Errorf("read addresses for %s: %w", name, err)
	}
	status := transport.NetworkStatus{Interface: name}
	for _, address := range addresses {
		status.Addresses = append(status.Addresses, address.String())
	}
	status.RXBytes, status.TXBytes = readInterfaceCounters(name)
	// Linux has no scoped routes: the modem interface and the system share one
	// default route, so both fields report the same line.
	status.DefaultRoute = linuxDefaultRoute(ctx)
	status.SystemDefaultRoute = status.DefaultRoute
	return status, nil
}

func (a *Adapter) CheckConnectivity(ctx context.Context, candidate device.Candidate) (transport.Connectivity, error) {
	status, err := a.Status(ctx, candidate)
	if err != nil {
		return transport.Connectivity{}, err
	}
	if len(status.Addresses) == 0 {
		return transport.Connectivity{Summary: "interface has no addresses", Detail: status.Interface}, nil
	}
	return transport.Connectivity{OK: true, Summary: "network interface is configured", Detail: status.Interface}, nil
}

// Diagnostics implements transport.NetworkDiagnostics with ip(8) inspection.
// A missing ip command or unparsable output degrades to empty values rather
// than failing the request.
func (a *Adapter) Diagnostics(ctx context.Context, candidate device.Candidate) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	moduleInterface := moduleNetworkInterface(candidate)
	return map[string]any{
		"linux_interfaces":    linuxInterfaces(ctx),
		"default_route":       linuxDefaultRoute(ctx),
		"module_interface":    moduleInterface,
		"usb_network_present": moduleInterface != "",
	}, nil
}

func linuxInterfaces(ctx context.Context) []string {
	output, err := exec.CommandContext(ctx, "ip", "-j", "addr", "show").Output()
	if err != nil {
		return nil
	}
	return parseLinuxInterfaces(output)
}

// parseLinuxInterfaces extracts the interface names from `ip -j addr show`
// output; unparsable output yields an empty list.
func parseLinuxInterfaces(output []byte) []string {
	var items []struct {
		IfName string `json:"ifname"`
	}
	if err := json.Unmarshal(output, &items); err != nil {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.IfName)
	}
	return names
}

// linuxDefaultRoute returns the default route line from ip(8) (for example
// "default via 192.168.225.1 dev enp0s20u2"), or "" when the command is
// unavailable or no default route exists.
func linuxDefaultRoute(ctx context.Context) string {
	output, err := exec.CommandContext(ctx, "ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// moduleNetworkInterface resolves the candidate's network interface name,
// preferring the candidate field over the discovery metadata.
func moduleNetworkInterface(candidate device.Candidate) string {
	name := strings.TrimSpace(candidate.NetworkInterface)
	if name == "" && candidate.Metadata != nil {
		name = candidate.Metadata["network_interface"]
	}
	return name
}

func (a *Adapter) Tunnel(ctx context.Context, candidate device.Candidate) (transport.Tunnel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openTUN("djonehub0")
}

func readInterfaceCounters(name string) (uint64, uint64) {
	data, err := os.ReadFile(filepath.Join("/sys/class/net", name, "statistics", "rx_bytes"))
	if err != nil {
		return 0, 0
	}
	rx, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	data, err = os.ReadFile(filepath.Join("/sys/class/net", name, "statistics", "tx_bytes"))
	if err != nil {
		return rx, 0
	}
	tx, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return rx, tx
}

func readHex(path string) uint16 {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(string(value)), 16, 16)
	if err != nil {
		return 0
	}
	return uint16(parsed)
}

func readLinkBase(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func mustGlob(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func unique(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func classifyMode(controlPath, driver string) string {
	control := strings.ToLower(filepath.Base(controlPath))
	driver = strings.ToLower(driver)
	switch {
	case strings.Contains(control, "mbim"), strings.Contains(driver, "mbim"):
		return "mbim"
	case strings.Contains(control, "qmi"), strings.Contains(driver, "qmi"), strings.Contains(driver, "gobinet"), strings.Contains(driver, "qcqmi"):
		return "qmi"
	case controlPath != "":
		return "unknown-control"
	default:
		return "at"
	}
}

func usbVendor(id uint16) string {
	switch id {
	case 0x2c7c:
		return "Quectel"
	case 0x2ca3:
		return "DJI"
	case 0x05c6:
		return "Qualcomm"
	default:
		return "USB modem"
	}
}

// usbProduct 识别受支持的 PID/VID 组合,与 darwin 平台的命名保持一致。
func usbProduct(vid, pid uint16) string {
	switch {
	case vid == 0x2c7c && pid == 0x0125:
		return "Quectel 4G Module"
	case vid == 0x2ca3 && pid == 0x4006:
		return "DJI 4G Module"
	default:
		return "USB modem"
	}
}

var _ transport.NetworkDiagnostics = (*Adapter)(nil)
