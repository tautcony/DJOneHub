package darwin

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/esim"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
)

type Adapter struct {
	*unsupported.Adapter
	networkMu         sync.Mutex
	networkInterfaces map[string]string
}

func New() *Adapter {
	return &Adapter{Adapter: unsupported.New("darwin", device.CapabilitySet{
		device.CapabilityDeviceStatus: "DJI/Quectel USB and AT serial discovery",
	}), networkInterfaces: make(map[string]string)}
}

// DetectEDL reports whether a Qualcomm emergency download device is present.
// EDL has no AT channel, so it must be detected directly from the USB tree.
func (a *Adapter) DetectEDL(ctx context.Context) (bool, error) {
	command := exec.CommandContext(ctx, "ioreg", "-r", "-c", "IOUSBHostDevice", "-l", "-w", "0")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("inspect USB devices for EDL mode: %w", err)
	}
	return containsUSBIdentity(string(output), 0x05c6, 0x9008), nil
}

func containsUSBIdentity(output string, vendorID, productID int) bool {
	starts := regexp.MustCompile(`(?m)^\+-o .*<class IOUSBHostDevice`).FindAllStringIndex(output, -1)
	if len(starts) == 0 {
		starts = [][]int{{0, 0}}
	}
	for index, start := range starts {
		end := len(output)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		block := output[start[0]:end]
		vendor, vendorOK := intProperty(block, "idVendor")
		product, productOK := intProperty(block, "idProduct")
		if vendorOK && productOK && vendor == vendorID && product == productID {
			return true
		}
	}
	return false
}

type usbIdentity struct {
	vendorID  int
	productID int
	key       string
	product   string
	vendor    string
	location  string
}

var supportedUSBIdentities = []struct {
	vendorID  int
	productID int
	key       string
	product   string
	vendor    string
}{
	{vendorID: 0x2ca3, productID: 0x4006, key: "dji", product: "DJI 4G Module", vendor: "DJI"},
	{vendorID: 0x2c7c, productID: 0x0125, key: "quectel", product: "Quectel 4G Module", vendor: "Quectel"},
}

func (a *Adapter) Discover(ctx context.Context) ([]device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	usb := discoverUSBIdentities(ctx)
	ports := discoverATPorts()
	if len(ports) == 0 && len(usb) == 0 {
		return nil, nil
	}
	if len(ports) == 0 {
		item := usb[0]
		return []device.Candidate{{
			Identity: device.Identity{
				StableID:         item.key + "/" + item.location,
				PhysicalLocation: item.location,
				VendorID:         fmt.Sprintf("%04x", item.vendorID),
				ProductID:        fmt.Sprintf("%04x", item.productID),
				Manufacturer:     item.vendor,
				Product:          item.product,
			},
			Metadata: map[string]string{"discovery": "macos-ioreg", "transport": "usb-at"},
		}}, nil
	}

	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		imei, err := modem.ProbeIMEICached(port, 2*time.Second)
		if err != nil || strings.TrimSpace(imei) == "" {
			continue
		}
		identity := device.Identity{
			StableID:     "serial/" + imei,
			IMEI:         imei,
			Product:      "DJI/Quectel AT modem",
			Manufacturer: "DJI/Quectel",
		}
		if len(usb) > 0 {
			identity.StableID = usb[0].key + "/" + usb[0].location
			identity.PhysicalLocation = usb[0].location
			identity.VendorID = fmt.Sprintf("%04x", usb[0].vendorID)
			identity.ProductID = fmt.Sprintf("%04x", usb[0].productID)
			identity.Manufacturer = usb[0].vendor
			identity.Product = usb[0].product
		}
		return []device.Candidate{{
			Identity: identity,
			ATPort:   port,
			Metadata: map[string]string{"discovery": "macos-ioreg-and-at-probe"},
		}}, nil
	}
	return nil, nil
}

func (a *Adapter) OpenAT(_ context.Context, candidate device.Candidate) (backend.ModemBackend, error) {
	transport, err := openUSBAT(usbDeviceIdentityForCandidate(candidate))
	if err != nil {
		return nil, err
	}
	commandBackend := backend.NewCommandBackend(transport, candidate.Identity)
	// 设备级 APDU 仲裁器: darwin 纯 AT eSIM 路径与 modem manager 路径共享的
	// 同一实例, 使 SIM 切换 barrier 与 APDU idle 等待在 USB AT 传输上生效。
	arbiter := apduarbiter.New(candidate.Identity.StableID, apduarbiter.Options{})
	if port, portErr := esim.NewATPort(candidate.Identity.StableID, arbiter, transport.Command, func(ctx context.Context) (string, error) {
		identity, err := commandBackend.Identity(ctx)
		return identity.IMEI, err
	}, func(ctx context.Context) (string, error) {
		sim, err := commandBackend.SIM(ctx)
		return sim.ICCID, err
	}); portErr == nil {
		commandBackend.SetESIMPort(port)
	}
	return commandBackend, nil
}

func usbDeviceIdentityForCandidate(candidate device.Candidate) usbDeviceIdentity {
	for _, supported := range supportedUSBIdentities {
		if supported.key == strings.Split(candidate.Identity.StableID, "/")[0] {
			return usbDeviceIdentity{
				Key: supported.key, Label: supported.key, VendorID: uint16(supported.vendorID), ProductID: uint16(supported.productID),
				LocationID: candidate.Identity.PhysicalLocation,
			}
		}
	}
	return usbDeviceIdentity{LocationID: candidate.Identity.PhysicalLocation}
}

func discoverATPorts() []string {
	var ports []string
	for _, pattern := range []string{
		"/dev/cu.usbmodem*",
		"/dev/cu.usbserial*",
		"/dev/cu.wchusbserial*",
	} {
		matches, err := filepath.Glob(pattern)
		if err == nil {
			ports = append(ports, matches...)
		}
	}
	sort.SliceStable(ports, func(i, j int) bool { return portScore(ports[i]) > portScore(ports[j]) })
	return ports
}

func portScore(port string) int {
	name := strings.ToLower(port)
	switch {
	case strings.Contains(name, "dji"), strings.Contains(name, "quectel"):
		return 100
	case strings.Contains(name, "usbmodem"):
		return 80
	case strings.Contains(name, "usbserial"):
		return 60
	default:
		return 0
	}
}

func discoverUSBIdentities(ctx context.Context) []usbIdentity {
	command := exec.CommandContext(ctx, "ioreg", "-r", "-c", "IOUSBHostInterface", "-l", "-w", "0")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	var result []usbIdentity
	for _, block := range strings.Split(string(output), "\n\n") {
		vendorID, okVendor := intProperty(block, "idVendor")
		productID, okProduct := intProperty(block, "idProduct")
		if !okVendor || !okProduct {
			continue
		}
		for _, supported := range supportedUSBIdentities {
			if supported.vendorID != vendorID || supported.productID != productID {
				continue
			}
			location := formatHexProperty(block, "locationID")
			if location == "" {
				location = "unknown"
			}
			result = append(result, usbIdentity{
				vendorID: vendorID, productID: productID, key: supported.key,
				product: supported.product, vendor: supported.vendor, location: location,
			})
		}
	}
	// ioreg emits one record per interface. Keep one physical identity.
	seen := make(map[string]bool, len(result))
	unique := result[:0]
	for _, item := range result {
		key := item.key + "/" + item.location
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, item)
	}
	return unique
}

func intProperty(block, name string) (int, bool) {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `"\s*=\s*(0[xX][0-9a-fA-F]+|[0-9]+)`)
	match := pattern.FindStringSubmatch(block)
	if len(match) != 2 {
		return 0, false
	}
	value := strings.TrimPrefix(strings.TrimPrefix(match[1], "0x"), "0X")
	base := 10
	if value != match[1] {
		base = 16
	}
	parsed, err := strconv.ParseInt(value, base, 64)
	return int(parsed), err == nil
}

func formatHexProperty(block, name string) string {
	value, ok := intProperty(block, name)
	if !ok {
		return ""
	}
	return fmt.Sprintf("0x%x", value)
}
