package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestObserveEDLReturnsStructuredUnsupported(t *testing.T) {
	_, err := New().ObserveEDL(context.Background(), device.Candidate{})
	var structured *derrors.Error
	if !errors.As(err, &structured) || structured.Code != derrors.CapabilityNotSupported {
		t.Fatalf("ObserveEDL() error=%v", err)
	}
}

func TestDiscoverSysfsCorrelatesATQMIAndNetworkPorts(t *testing.T) {
	requireLinuxSysfsPathHost(t)
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "1-2")
	interfaceRoot := filepath.Join(deviceRoot, "1-2:1.0")
	for _, dir := range []string{
		deviceRoot,
		filepath.Join(interfaceRoot, "tty", "ttyUSB2"),
		filepath.Join(interfaceRoot, "usbmisc", "cdc-wdm0"),
		filepath.Join(interfaceRoot, "net", "wwan0"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{"idVendor": "2c7c\n", "idProduct": "0125\n"} {
		if err := os.WriteFile(filepath.Join(deviceRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../../../drivers/qmi_wwan", filepath.Join(interfaceRoot, "driver")); err != nil {
		t.Fatal(err)
	}
	candidates, err := discoverSysfs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.ATPort != "/dev/ttyUSB2" || candidate.ControlPath != "/dev/cdc-wdm0" || candidate.NetworkInterface != "wwan0" {
		t.Fatalf("candidate ports = %#v", candidate)
	}
	if len(candidate.ATPorts) != 1 || candidate.ATPorts[0] != "/dev/ttyUSB2" {
		t.Fatalf("candidate at_ports = %#v", candidate.ATPorts)
	}
	if candidate.Metadata["mode"] != "qmi" || candidate.Identity.VendorID != "2c7c" {
		t.Fatalf("candidate metadata = %#v identity=%#v", candidate.Metadata, candidate.Identity)
	}
}

// TestDiscoverSysfsOptionDriverMultiPort mirrors an EC25 dongle bound to the
// option driver: one tty port per interface, as a direct child of the
// interface directory. The default selection is the first sorted port, the
// full list is kept for AT probing.
func TestDiscoverSysfsOptionDriverMultiPort(t *testing.T) {
	requireLinuxSysfsPathHost(t)
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "1-4.1")
	ports := []string{"ttyUSB0", "ttyUSB1", "ttyUSB2", "ttyUSB3"}
	for i, port := range ports {
		if err := os.MkdirAll(filepath.Join(deviceRoot, fmt.Sprintf("1-4.1:1.%d", i), port), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{"idVendor": "2c7c\n", "idProduct": "0125\n"} {
		if err := os.WriteFile(filepath.Join(deviceRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := discoverSysfs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.ATPort != "/dev/ttyUSB0" {
		t.Fatalf("default at port = %q, want /dev/ttyUSB0", candidate.ATPort)
	}
	want := []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "/dev/ttyUSB2", "/dev/ttyUSB3"}
	if !slices.Equal(candidate.ATPorts, want) {
		t.Fatalf("candidate at_ports = %#v, want %#v", candidate.ATPorts, want)
	}
	if candidate.Identity.Product != "Quectel 4G Module" {
		t.Fatalf("product = %q, want Quectel 4G Module", candidate.Identity.Product)
	}
}

func TestUSBProductNames(t *testing.T) {
	cases := []struct {
		vid, pid uint16
		want     string
	}{
		{0x2c7c, 0x0125, "Quectel 4G Module"},
		{0x2ca3, 0x4006, "DJI 4G Module"},
		{0x2c7c, 0x9999, "USB modem"},
		{0x05c6, 0x9008, "USB modem"},
	}
	for _, tc := range cases {
		if got := usbProduct(tc.vid, tc.pid); got != tc.want {
			t.Fatalf("usbProduct(%04x:%04x) = %q, want %q", tc.vid, tc.pid, got, tc.want)
		}
	}
}

func TestChooseATPort(t *testing.T) {
	ports := []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "/dev/ttyUSB2", "/dev/ttyUSB3"}

	// First responding port wins.
	responds := map[string]bool{"/dev/ttyUSB2": true}
	probe := func(port string) (bool, bool) {
		if responds[port] {
			return true, false
		}
		return false, false
	}
	if got := chooseATPort(ports, probe); got != "/dev/ttyUSB2" {
		t.Fatalf("chooseATPort = %q, want /dev/ttyUSB2", got)
	}

	// A port held busy wins when nothing answers AT (ModemManager case).
	probe = func(port string) (bool, bool) {
		if port == "/dev/ttyUSB2" {
			return false, true
		}
		return false, false
	}
	if got := chooseATPort(ports, probe); got != "/dev/ttyUSB2" {
		t.Fatalf("chooseATPort = %q, want busy /dev/ttyUSB2", got)
	}

	// No responder and no busy port: empty result, caller keeps the default.
	probe = func(port string) (bool, bool) { return false, false }
	if got := chooseATPort(ports, probe); got != "" {
		t.Fatalf("chooseATPort = %q, want empty", got)
	}
}

func TestIsBusySerialErr(t *testing.T) {
	for _, msg := range []string{
		"Serial port busy",
		"device or resource busy",
		"resource busy",
	} {
		if !isBusySerialErr(errors.New(msg)) {
			t.Fatalf("isBusySerialErr(%q) = false, want true", msg)
		}
	}
	if isBusySerialErr(errors.New("imei probe timeout")) || isBusySerialErr(nil) {
		t.Fatal("isBusySerialErr misclassified non-busy error")
	}
}

func TestDiscoverSysfsAllowsControlDeviceWithoutAT(t *testing.T) {
	requireLinuxSysfsPathHost(t)
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "2-1")
	interfaceRoot := filepath.Join(deviceRoot, "2-1:1.0")
	if err := os.MkdirAll(filepath.Join(interfaceRoot, "usbmisc", "cdc-wdm0"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"idVendor": "2c7c\n", "idProduct": "0800\n"} {
		if err := os.WriteFile(filepath.Join(deviceRoot, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := discoverSysfs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ATPort != "" {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func requireLinuxSysfsPathHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot represent Linux sysfs interface names containing colons")
	}
}

func TestDetectEDL(t *testing.T) {
	t.Run("qualcomm edl device is detected", func(t *testing.T) {
		root := t.TempDir()
		deviceRoot := filepath.Join(root, "1-2")
		if err := os.MkdirAll(deviceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		// Root hubs are named like usb1 and must be skipped, not treated as
		// USB devices.
		if err := os.MkdirAll(filepath.Join(root, "usb1"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"idVendor": "05c6\n", "idProduct": "9008\n"} {
			if err := os.WriteFile(filepath.Join(deviceRoot, name), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		adapter := &Adapter{sysfsRoot: root}
		detected, err := adapter.DetectEDL(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !detected {
			t.Fatal("DetectEDL = false, want true for Qualcomm 05c6:9008")
		}
	})

	t.Run("non-EDL device is not detected", func(t *testing.T) {
		root := t.TempDir()
		deviceRoot := filepath.Join(root, "1-2")
		if err := os.MkdirAll(deviceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"idVendor": "2c7c\n", "idProduct": "0125\n"} {
			if err := os.WriteFile(filepath.Join(deviceRoot, name), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		adapter := &Adapter{sysfsRoot: root}
		detected, err := adapter.DetectEDL(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if detected {
			t.Fatal("DetectEDL = true, want false for 2c7c:0125")
		}
	})

	t.Run("missing sysfs root is not an error", func(t *testing.T) {
		adapter := &Adapter{sysfsRoot: filepath.Join(t.TempDir(), "missing")}
		detected, err := adapter.DetectEDL(context.Background())
		if err != nil {
			t.Fatalf("DetectEDL = %v, want nil error for missing root", err)
		}
		if detected {
			t.Fatal("DetectEDL = true, want false for missing root")
		}
	})
}

func TestParseLinuxInterfaces(t *testing.T) {
	output := []byte(`[{"ifname":"lo","addr_info":[{"family":"inet","local":"127.0.0.1"}]},{"ifname":"wwan0","addr_info":[]}]`)
	if got := parseLinuxInterfaces(output); !slices.Equal(got, []string{"lo", "wwan0"}) {
		t.Fatalf("parseLinuxInterfaces = %#v, want [lo wwan0]", got)
	}
	if got := parseLinuxInterfaces([]byte("not json")); len(got) != 0 {
		t.Fatalf("parseLinuxInterfaces(garbage) = %#v, want empty", got)
	}
}

func TestModuleNetworkInterfaceResolution(t *testing.T) {
	cases := []struct {
		name      string
		candidate device.Candidate
		want      string
	}{
		{"candidate field wins", device.Candidate{NetworkInterface: "wwan0"}, "wwan0"},
		{"metadata fallback", device.Candidate{Metadata: map[string]string{"network_interface": "wwan0"}}, "wwan0"},
		{"field beats metadata", device.Candidate{NetworkInterface: "eth9", Metadata: map[string]string{"network_interface": "wwan0"}}, "eth9"},
		{"empty candidate", device.Candidate{}, ""},
		{"whitespace is trimmed", device.Candidate{NetworkInterface: "  wwan0  "}, "wwan0"},
	}
	for _, tc := range cases {
		if got := moduleNetworkInterface(tc.candidate); got != tc.want {
			t.Fatalf("%s: moduleNetworkInterface = %q, want %q", tc.name, got, tc.want)
		}
	}
}
