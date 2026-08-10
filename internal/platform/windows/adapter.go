package windows

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/transport"
)

const (
	probeTimeout  = 2 * time.Second
	probeCooldown = 10 * time.Minute
	probeBudget   = 20 * time.Second
)

// Adapter discovers the modem's Windows COM port and lets the existing AT
// backend own all protocol operations. This is the same application path as
// Linux serial mode; Windows does not need the macOS libusb bridge.
type Adapter struct {
	*unsupported.Adapter

	probeMu   sync.Mutex
	probeFail map[string]time.Time
	listPorts func() ([]string, error)
	probeIMEI func(string, time.Duration) (string, error)
}

func New() *Adapter {
	return &Adapter{
		Adapter: unsupported.New("windows", device.CapabilitySet{
			device.CapabilityDeviceStatus:       "Windows COM/AT modem discovery",
			device.CapabilityNetworkStatus:      "Windows host network inspection when an interface is configured",
			device.CapabilityNetworkDiagnostics: "Windows network interface inspection",
		}),
		probeFail: make(map[string]time.Time),
		listPorts: serial.GetPortsList,
		probeIMEI: modem.ProbeIMEICached,
	}
}

func (a *Adapter) PlatformCapabilities(context.Context) device.CapabilitySet {
	return a.Adapter.Capabilities.Clone()
}
func (a *Adapter) EnterEDL(context.Context, device.Candidate) error {
	return unsupported.Unsupported(string(device.CapabilityFirmwareEDLSwitch), "enter_edl")
}
func (a *Adapter) FindEDL(context.Context, device.Candidate) (device.Candidate, error) {
	return device.Candidate{}, unsupported.Unsupported(string(device.CapabilityFirmwareEDLSwitch), "find_edl")
}
func (a *Adapter) FindOriginal(context.Context, device.Candidate) (device.Candidate, error) {
	return device.Candidate{}, unsupported.Unsupported(string(device.CapabilityFirmwareEDLSwitch), "find_original")
}
func (a *Adapter) ReadNAND(context.Context, device.Candidate, transport.FirehoseReadRequest) (transport.FirehoseReadResult, error) {
	return transport.FirehoseReadResult{}, unsupported.Unsupported(string(device.CapabilityFirmwareNANDBackup), "read_nand")
}
func (a *Adapter) Reset(context.Context, device.Candidate) error {
	return unsupported.Unsupported(string(device.CapabilityFirmwareNANDBackup), "reset")
}

func (a *Adapter) Discover(ctx context.Context) ([]device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listPorts := a.listPorts
	if listPorts == nil {
		listPorts = serial.GetPortsList
	}
	ports, err := listPorts()
	if err != nil {
		return nil, fmt.Errorf("enumerate Windows COM ports: %w", err)
	}
	if explicit := strings.TrimSpace(os.Getenv("DJONEHUB_AT_PORT")); explicit != "" {
		ports = append([]string{explicit}, ports...)
	}
	ports = uniquePorts(ports)
	sort.SliceStable(ports, func(i, j int) bool { return windowsPortScore(ports[i]) > windowsPortScore(ports[j]) })

	probe := a.probeIMEI
	if probe == nil {
		probe = modem.ProbeIMEICached
	}
	started := time.Now()
	result := make([]device.Candidate, 0, 1)
	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Since(started) >= probeBudget {
			break
		}
		if a.probeFailedRecently(port) {
			continue
		}
		imei, probeErr := probe(port, probeTimeout)
		if probeErr != nil || strings.TrimSpace(imei) == "" {
			a.rememberProbeFailure(port)
			continue
		}
		result = append(result, device.Candidate{
			Identity: device.Identity{
				StableID:     "windows/serial/" + strings.TrimSpace(imei),
				IMEI:         strings.TrimSpace(imei),
				Manufacturer: "DJI/Quectel",
				Product:      "AT modem (Windows COM)",
			},
			ATPort:   port,
			ATPorts:  append([]string(nil), ports...),
			Metadata: map[string]string{"discovery": "windows-com-at", "transport": "serial-at"},
		})
		break
	}
	return result, nil
}

func uniquePorts(ports []string) []string {
	seen := make(map[string]struct{}, len(ports))
	result := make([]string, 0, len(ports))
	for _, port := range ports {
		port = strings.TrimSpace(port)
		if port == "" {
			continue
		}
		key := strings.ToUpper(port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, port)
	}
	return result
}

func windowsPortScore(port string) int {
	name := strings.ToUpper(strings.TrimSpace(port))
	if !strings.HasPrefix(name, "COM") {
		return 0
	}
	number, err := strconv.Atoi(strings.TrimPrefix(name, "COM"))
	if err != nil || number <= 0 {
		return 0
	}
	// Lower COM numbers are commonly the modem's stable interface, while
	// virtual debug ports tend to be assigned later.
	return 100000 - number
}

func (a *Adapter) probeFailedRecently(port string) bool {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	failedAt, ok := a.probeFail[port]
	if !ok {
		return false
	}
	if time.Since(failedAt) < probeCooldown {
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
	name := strings.TrimSpace(candidate.NetworkInterface)
	if name == "" && candidate.Metadata != nil {
		name = strings.TrimSpace(candidate.Metadata["network_interface"])
	}
	if name == "" {
		name = strings.TrimSpace(os.Getenv("DJONEHUB_NETWORK_INTERFACE"))
	}
	if name == "" {
		return transport.NetworkStatus{}, derrors.CapabilityMissing(string(device.CapabilityNetworkStatus), "network_host_status", "Windows modem network interface is not configured")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return transport.NetworkStatus{}, fmt.Errorf("read Windows network interface %s: %w", name, err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return transport.NetworkStatus{}, fmt.Errorf("read addresses for %s: %w", name, err)
	}
	result := transport.NetworkStatus{Interface: name}
	for _, address := range addresses {
		result.Addresses = append(result.Addresses, address.String())
	}
	return result, nil
}

func (a *Adapter) CheckConnectivity(ctx context.Context, candidate device.Candidate) (transport.Connectivity, error) {
	status, err := a.Status(ctx, candidate)
	if err != nil {
		return transport.Connectivity{}, err
	}
	if len(status.Addresses) == 0 {
		return transport.Connectivity{Summary: "Windows network interface has no addresses", Detail: status.Interface}, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://connectivitycheck.gstatic.com/generate_204", nil)
	if err != nil {
		return transport.Connectivity{}, err
	}
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return transport.Connectivity{Summary: "Windows network interface is configured but internet access failed", Detail: err.Error()}, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return transport.Connectivity{Summary: "Windows internet check returned an unexpected response", Detail: response.Status}, nil
	}
	return transport.Connectivity{OK: true, Summary: "Windows internet access is available", Detail: status.Interface}, nil
}

func (a *Adapter) Diagnostics(ctx context.Context, candidate device.Candidate) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate Windows network interfaces: %w", err)
	}
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			names = append(names, iface.Name)
		}
	}
	sort.Strings(names)
	return map[string]any{
		"windows_interfaces": names,
		"module_interface":   strings.TrimSpace(candidate.NetworkInterface),
		"serial_port":        candidate.ATPort,
	}, nil
}

var _ transport.NetworkDiagnostics = (*Adapter)(nil)
