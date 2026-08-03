package darwin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/transport"
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

func (a *Adapter) Diagnostics(ctx context.Context, _ device.Candidate) (map[string]any, error) {
	interfaces := macInterfaces(ctx)
	route := macDefaultRoute(ctx)
	return map[string]any{
		"mac_interfaces":      interfaces,
		"default_route":       route,
		"usb_network_present": hasUSBNetwork(interfaces),
	}, nil
}

func (a *Adapter) CheckRoute(ctx context.Context, _ device.Candidate, kind string) (transport.Connectivity, error) {
	if kind == "proxy" {
		proxyURL, _ := url.Parse("http://127.0.0.1:7890")
		client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
		request, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://www.google.com/generate_204", nil)
		if err != nil {
			return transport.Connectivity{Summary: "proxy request could not be created", Detail: err.Error()}, nil
		}
		response, err := client.Do(request)
		if err != nil {
			return transport.Connectivity{Summary: "proxy is unavailable", Detail: "127.0.0.1:7890: " + err.Error()}, nil
		}
		defer response.Body.Close()
		return transport.Connectivity{OK: response.StatusCode >= 200 && response.StatusCode < 400, Summary: "proxy check completed", Detail: response.Status}, nil
	}
	route := macDefaultRoute(ctx)
	interfaces := macInterfaces(ctx)
	if route.Interface == "" {
		return transport.Connectivity{Summary: "no default route was reported", Detail: "macOS returned no default route"}, nil
	}
	for _, item := range interfaces {
		if item.Name == route.Interface && item.Kind == "ethernet" && item.Name != "en0" && item.Status == "active" {
			return transport.Connectivity{OK: true, Summary: "the 4G interface is the active default route", Detail: fmt.Sprintf("%s -> %s (%s)", route.Interface, route.Gateway, item.IPv4)}, nil
		}
	}
	return transport.Connectivity{Summary: "the 4G interface is not the active default route", Detail: fmt.Sprintf("default route %s -> %s", route.Interface, route.Gateway)}, nil
}

func (a *Adapter) CellularPolicy(context.Context, device.Candidate) (transport.CellularPolicy, error) {
	a.policyMu.Lock()
	defer a.policyMu.Unlock()
	if err := a.loadPolicyLocked(); err != nil {
		return transport.CellularPolicy{}, err
	}
	return transport.CellularPolicy{ForceOff: a.force4GOff, Services: append([]string(nil), a.disabled4GServices...)}, nil
}

func (a *Adapter) SetCellularPolicy(ctx context.Context, _ device.Candidate, forceOff bool) (transport.CellularPolicy, error) {
	a.policyMu.Lock()
	defer a.policyMu.Unlock()
	services, err := networkServices(ctx)
	if err != nil {
		return transport.CellularPolicy{}, err
	}
	changed := make([]string, 0)
	for _, service := range services {
		if !isCellularService(service) {
			continue
		}
		if err := setNetworkService(ctx, service.Name, !forceOff); err != nil {
			return transport.CellularPolicy{}, err
		}
		if forceOff {
			changed = append(changed, service.Name)
		}
	}
	a.force4GOff, a.disabled4GServices = forceOff, changed
	a.policyLoaded = true
	if a.policyStore != nil {
		if err := a.policyStore.Write(map[string]any{"force_off": forceOff, "services": changed}); err != nil {
			return transport.CellularPolicy{}, err
		}
	}
	return transport.CellularPolicy{ForceOff: forceOff, Services: append([]string(nil), changed...)}, nil
}

func (a *Adapter) loadPolicyLocked() error {
	if a.policyLoaded {
		return nil
	}
	a.policyLoaded = true
	if a.policyStore == nil {
		return nil
	}
	var value struct {
		ForceOff bool     `json:"force_off"`
		Services []string `json:"services"`
	}
	if err := a.policyStore.Read(&value); err != nil {
		return err
	}
	a.force4GOff = value.ForceOff
	a.disabled4GServices = append([]string(nil), value.Services...)
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) string {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(output)
}
func macInterfaces(ctx context.Context) []macInterface {
	output := commandOutput(ctx, "ifconfig")
	result := []macInterface{}
	for _, block := range strings.Split(output, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 {
			continue
		}
		fields := strings.Fields(lines[0])
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		if name == "" || strings.HasPrefix(name, "lo") || strings.HasPrefix(name, "utun") {
			continue
		}
		item := macInterface{Name: name, Status: "unknown", Kind: interfaceKind(name)}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "status:") {
				item.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
			}
			if strings.HasPrefix(line, "inet ") {
				parts := strings.Fields(line)
				if len(parts) > 1 {
					item.IPv4 = parts[1]
				}
			}
		}
		result = append(result, item)
	}
	return result
}
func macDefaultRoute(ctx context.Context) macRoute {
	result := macRoute{}
	for _, line := range strings.Split(commandOutput(ctx, "route", "-n", "get", "default"), "\n") {
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
func hasUSBNetwork(items []macInterface) bool {
	for _, item := range items {
		if item.Kind == "ethernet" && item.Name != "en0" && item.Status == "active" {
			return true
		}
	}
	return false
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
	header := regexp.MustCompile(`^\((\*|\d+)\)\s+(.+)$`)
	detail := regexp.MustCompile(`^\(Hardware Port:\s*([^,]+),\s*Device:\s*([^)]+)\)$`)
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	result := []networkService{}
	for index := 0; index+1 < len(lines); index++ {
		h, d := header.FindStringSubmatch(strings.TrimSpace(lines[index])), detail.FindStringSubmatch(strings.TrimSpace(lines[index+1]))
		if len(h) == 3 && len(d) == 3 {
			result = append(result, networkService{Name: strings.TrimSpace(h[2]), HardwarePort: strings.TrimSpace(d[1]), Device: strings.TrimSpace(d[2]), Disabled: h[1] == "*"})
		}
	}
	return result, nil
}
func isCellularService(item networkService) bool {
	return strings.EqualFold(item.HardwarePort, "Baiwang") && regexp.MustCompile(`^en\d+$`).MatchString(item.Device)
}
func setNetworkService(ctx context.Context, name string, enabled bool) error {
	value := "off"
	if enabled {
		value = "on"
	}
	output, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", "-setnetworkserviceenabled", name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}

var _ transport.NetworkDiagnostics = (*Adapter)(nil)
