package linux

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/domain/errors"
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
}

func New() *Adapter {
	return &Adapter{Adapter: unsupported.New("linux", device.CapabilitySet{
		device.CapabilityDeviceStatus:  "USB/sysfs modem discovery",
		device.CapabilityNetworkStatus: "Linux network interface status",
	}), sysfsRoot: "/sys/bus/usb/devices"}
}

func (a *Adapter) Discover(ctx context.Context) ([]device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return discoverSysfs(ctx, a.sysfsRoot)
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
		Product:          "USB modem",
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
		candidate.ATPort = atPorts[0]
	}
	return candidate, true
}

func (a *Adapter) Status(ctx context.Context, candidate device.Candidate) (transport.NetworkStatus, error) {
	if err := ctx.Err(); err != nil {
		return transport.NetworkStatus{}, err
	}
	name := strings.TrimSpace(candidate.NetworkInterface)
	if name == "" && candidate.Metadata != nil {
		name = candidate.Metadata["network_interface"]
	}
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
