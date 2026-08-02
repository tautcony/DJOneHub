package linux

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSysfsCorrelatesATQMIAndNetworkPorts(t *testing.T) {
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
	if candidate.Metadata["mode"] != "qmi" || candidate.Identity.VendorID != "2c7c" {
		t.Fatalf("candidate metadata = %#v identity=%#v", candidate.Metadata, candidate.Identity)
	}
}

func TestDiscoverSysfsAllowsControlDeviceWithoutAT(t *testing.T) {
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
