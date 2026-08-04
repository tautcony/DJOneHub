package darwin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/transport"
)

const connectivityCheckURL = "http://connectivitycheck.gstatic.com/generate_204"

var (
	ifconfigHeaderPattern = regexp.MustCompile(`^([[:alnum:]_.-]+):\s+flags=`)
	interfaceNamePattern  = regexp.MustCompile(`"IOInterfaceName"\s*=\s*"([^"]+)"`)
	networkHeaderPattern  = regexp.MustCompile(`^\((\*|\d+)\)\s+(.+)$`)
	networkDetailPattern  = regexp.MustCompile(`^\(Hardware Port:\s*([^,]+),\s*Device:\s*([^)]+)\)$`)
)

type macInterface struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	IPv4   string `json:"ipv4"`
	Kind   string `json:"kind"`
}

type macRoute struct {
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
}

func (a *Adapter) Status(ctx context.Context, candidate device.Candidate) (transport.NetworkStatus, error) {
	name := a.moduleNetworkInterface(ctx, candidate)
	if name == "" {
		return transport.NetworkStatus{}, fmt.Errorf("QDC507 ECM network interface was not found")
	}
	item, addresses, err := macInterfaceDetails(ctx, name)
	if err != nil {
		return transport.NetworkStatus{}, err
	}
	rxBytes, txBytes := macInterfaceCounters(ctx, name)
	defaultRoute := macScopedDefaultRoute(ctx, name)
	systemRoute := macDefaultRoute(ctx)
	return transport.NetworkStatus{
		Interface:          item.Name,
		Addresses:          addresses,
		DefaultRoute:       formatRoute(defaultRoute),
		SystemDefaultRoute: formatRoute(systemRoute),
		RXBytes:            rxBytes,
		TXBytes:            txBytes,
	}, nil
}

func (a *Adapter) NetworkTraffic(ctx context.Context, candidate device.Candidate) (uint64, uint64, error) {
	name := a.moduleNetworkInterface(ctx, candidate)
	if name == "" {
		return 0, 0, fmt.Errorf("QDC507 ECM network interface was not found")
	}
	rxBytes, txBytes := macInterfaceCounters(ctx, name)
	return rxBytes, txBytes, nil
}

func (a *Adapter) CheckConnectivity(ctx context.Context, candidate device.Candidate) (transport.Connectivity, error) {
	status, err := a.Status(ctx, candidate)
	if err != nil {
		return transport.Connectivity{Summary: "ECM interface is unavailable", Detail: err.Error()}, nil
	}
	item, _, err := macInterfaceDetails(ctx, status.Interface)
	if err != nil {
		return transport.Connectivity{Summary: "ECM interface could not be inspected", Detail: err.Error()}, nil
	}
	if item.Status != "active" {
		return transport.Connectivity{Summary: "ECM interface is not active", Detail: status.Interface}, nil
	}

	source := preferredSourceAddress(status.Addresses)
	if source == nil {
		return transport.Connectivity{Summary: "ECM interface has no usable IP address", Detail: status.Interface}, nil
	}
	scopedRoute := macScopedDefaultRoute(ctx, status.Interface)
	if scopedRoute.Interface != status.Interface || scopedRoute.Gateway == "" {
		return transport.Connectivity{Summary: "ECM interface has no scoped default route", Detail: status.Interface}, nil
	}

	dialer := &net.Dialer{Timeout: 6 * time.Second, LocalAddr: &net.TCPAddr{IP: source}}
	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: dialer.DialContext,
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connectivityCheckURL, nil)
	if err != nil {
		return transport.Connectivity{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return transport.Connectivity{
			Summary: "ECM interface is configured but internet access failed",
			Detail:  fmt.Sprintf("%s via %s: %v", status.Interface, scopedRoute.Gateway, err),
		}, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return transport.Connectivity{
			Summary: "ECM internet check returned an unexpected response",
			Detail:  fmt.Sprintf("%s via %s: %s", status.Interface, scopedRoute.Gateway, response.Status),
		}, nil
	}
	return transport.Connectivity{
		OK:      true,
		Summary: "ECM internet access is available",
		Detail:  fmt.Sprintf("%s (%s) via %s", status.Interface, source, scopedRoute.Gateway),
	}, nil
}

func (a *Adapter) Diagnostics(ctx context.Context, candidate device.Candidate) (map[string]any, error) {
	interfaces := macInterfaces(ctx)
	route := macDefaultRoute(ctx)
	moduleInterface := a.moduleNetworkInterface(ctx, candidate)
	return map[string]any{
		"mac_interfaces":      interfaces,
		"default_route":       route,
		"module_interface":    moduleInterface,
		"usb_network_present": moduleInterface != "",
	}, nil
}

func commandOutput(ctx context.Context, name string, args ...string) string {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func macInterfaces(ctx context.Context) []macInterface {
	return parseIfconfig(commandOutput(ctx, "ifconfig", "-a"))
}

func parseIfconfig(output string) []macInterface {
	var result []macInterface
	var current *macInterface
	appendCurrent := func() {
		if current == nil || current.Name == "" || strings.HasPrefix(current.Name, "lo") || strings.HasPrefix(current.Name, "utun") {
			return
		}
		result = append(result, *current)
	}
	for _, rawLine := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if match := ifconfigHeaderPattern.FindStringSubmatch(rawLine); len(match) == 2 {
			appendCurrent()
			current = &macInterface{Name: match[1], Status: "unknown", Kind: interfaceKind(match[1])}
			continue
		}
		if current == nil {
			continue
		}
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "status:") {
			current.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				current.IPv4 = parts[1]
			}
		}
	}
	appendCurrent()
	return result
}

func macInterfaceDetails(ctx context.Context, name string) (macInterface, []string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return macInterface{}, nil, fmt.Errorf("read ECM interface %s: %w", name, err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return macInterface{}, nil, fmt.Errorf("read ECM addresses for %s: %w", name, err)
	}
	items := parseIfconfig(commandOutput(ctx, "ifconfig", name))
	item := macInterface{Name: name, Status: "unknown", Kind: interfaceKind(name)}
	if len(items) > 0 {
		item = items[0]
	}
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.String())
	}
	sort.Strings(values)
	return item, values, nil
}

func (a *Adapter) moduleNetworkInterface(ctx context.Context, candidate device.Candidate) string {
	cacheKey := candidate.StableID()
	a.networkMu.Lock()
	cached := a.networkInterfaces[cacheKey]
	a.networkMu.Unlock()
	if cached != "" {
		if _, err := net.InterfaceByName(cached); err == nil {
			return cached
		}
		a.networkMu.Lock()
		delete(a.networkInterfaces, cacheKey)
		a.networkMu.Unlock()
	}
	if name := strings.TrimSpace(candidate.NetworkInterface); name != "" {
		if _, err := net.InterfaceByName(name); err == nil {
			return a.cacheNetworkInterface(cacheKey, name)
		}
	}
	output := commandOutput(ctx, "ioreg", "-r", "-c", "IOUSBHostInterface", "-l", "-w", "0")
	if names := parseECMInterfaceNames(output, candidate); len(names) > 0 {
		return a.cacheNetworkInterface(cacheKey, names[0])
	}
	services, _ := networkServices(ctx)
	for _, service := range services {
		if isModuleNetworkService(service) {
			if _, err := net.InterfaceByName(service.Device); err == nil {
				return a.cacheNetworkInterface(cacheKey, service.Device)
			}
		}
	}
	return ""
}

func (a *Adapter) cacheNetworkInterface(key, name string) string {
	a.networkMu.Lock()
	a.networkInterfaces[key] = name
	a.networkMu.Unlock()
	return name
}

func parseECMInterfaceNames(output string, candidate device.Candidate) []string {
	var result []string
	for _, block := range strings.Split("\n"+output, "\n+-o ") {
		class, classOK := intProperty(block, "bInterfaceClass")
		subclass, subclassOK := intProperty(block, "bInterfaceSubClass")
		if !classOK || !subclassOK || class != 2 || subclass != 6 || !usbBlockMatchesCandidate(block, candidate) {
			continue
		}
		match := interfaceNamePattern.FindStringSubmatch(block)
		if len(match) == 2 && regexp.MustCompile(`^en\d+$`).MatchString(match[1]) {
			result = append(result, match[1])
		}
	}
	sort.Strings(result)
	return uniqueStrings(result)
}

func usbBlockMatchesCandidate(block string, candidate device.Candidate) bool {
	vendorID, vendorOK := intProperty(block, "idVendor")
	productID, productOK := intProperty(block, "idProduct")
	if !vendorOK || !productOK || !supportedUSBNetworkIdentity(vendorID, productID) {
		return false
	}
	if value, err := strconv.ParseUint(strings.TrimSpace(candidate.Identity.VendorID), 16, 16); err == nil && int(value) != vendorID {
		return false
	}
	if value, err := strconv.ParseUint(strings.TrimSpace(candidate.Identity.ProductID), 16, 16); err == nil && int(value) != productID {
		return false
	}
	candidateLocation := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(candidate.Identity.PhysicalLocation), "0x"), "0X")
	if candidateLocation == "" || candidateLocation == "unknown" {
		return true
	}
	wantLocation, err := strconv.ParseUint(candidateLocation, 16, 32)
	if err != nil {
		return true
	}
	location, ok := intProperty(block, "locationID")
	return ok && uint64(location) == wantLocation
}

func supportedUSBNetworkIdentity(vendorID, productID int) bool {
	for _, identity := range supportedUSBIdentities {
		if identity.vendorID == vendorID && identity.productID == productID {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func macDefaultRoute(ctx context.Context) macRoute {
	return parseRoute(commandOutput(ctx, "route", "-n", "get", "default"))
}

func macScopedDefaultRoute(ctx context.Context, name string) macRoute {
	return parseRoute(commandOutput(ctx, "route", "-n", "get", "-ifscope", name, "default"))
}

func parseRoute(output string) macRoute {
	result := macRoute{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gateway:") {
			result.Gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
		if strings.HasPrefix(line, "interface:") {
			result.Interface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	return result
}

func formatRoute(route macRoute) string {
	if route.Interface == "" {
		return ""
	}
	if route.Gateway == "" {
		return route.Interface
	}
	return fmt.Sprintf("%s via %s", route.Interface, route.Gateway)
}

func preferredSourceAddress(addresses []string) net.IP {
	var ipv6 net.IP
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address)
		if err != nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.To4() != nil {
			return ip
		}
		if ipv6 == nil {
			ipv6 = ip
		}
	}
	return ipv6
}

func macInterfaceCounters(ctx context.Context, name string) (uint64, uint64) {
	return parseInterfaceCounters(commandOutput(ctx, "netstat", "-ibn", "-I", name), name)
}

func parseInterfaceCounters(output, name string) (uint64, uint64) {
	var ibytesIndex, obytesIndex = -1, -1
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Name" {
			for index, field := range fields {
				switch field {
				case "Ibytes":
					ibytesIndex = index
				case "Obytes":
					obytesIndex = index
				}
			}
			continue
		}
		if fields[0] != name || ibytesIndex < 0 || obytesIndex < 0 || len(fields) <= obytesIndex || !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[ibytesIndex], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[obytesIndex], 10, 64)
		return rxBytes, txBytes
	}
	return 0, 0
}

func interfaceKind(name string) string {
	switch {
	case strings.HasPrefix(name, "en"):
		return "ethernet"
	case strings.HasPrefix(name, "bridge"):
		return "bridge"
	case strings.HasPrefix(name, "awdl"), strings.HasPrefix(name, "llw"), strings.HasPrefix(name, "ap"):
		return "apple-wireless"
	default:
		return "other"
	}
}

type networkService struct {
	Name, HardwarePort, Device string
	Disabled                   bool
}

func networkServices(ctx context.Context) ([]networkService, error) {
	output, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", "-listnetworkserviceorder").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read macOS network services: %s", strings.TrimSpace(string(output)))
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	result := []networkService{}
	for index := 0; index+1 < len(lines); index++ {
		header := networkHeaderPattern.FindStringSubmatch(strings.TrimSpace(lines[index]))
		detail := networkDetailPattern.FindStringSubmatch(strings.TrimSpace(lines[index+1]))
		if len(header) == 3 && len(detail) == 3 {
			result = append(result, networkService{
				Name: strings.TrimSpace(header[2]), HardwarePort: strings.TrimSpace(detail[1]),
				Device: strings.TrimSpace(detail[2]), Disabled: header[1] == "*",
			})
		}
	}
	return result, nil
}

func isModuleNetworkService(item networkService) bool {
	return strings.EqualFold(item.HardwarePort, "Baiwang") && regexp.MustCompile(`^en\d+$`).MatchString(item.Device)
}

var _ transport.NetworkDiagnostics = (*Adapter)(nil)
var _ transport.NetworkTrafficReader = (*Adapter)(nil)
