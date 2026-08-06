package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func readDevicesFromFile(t *testing.T, path string) []DeviceConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var out struct {
		Devices []DeviceConfig `yaml:"devices"`
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return out.Devices
}

func TestDeviceFilePersistsQMIProxyFields(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 7575
devices: []
`)

	err := AddDeviceInFile(path, DeviceConfig{
		ID:                 "dev-qmi",
		Interface:          "wwan0",
		ControlDevice:      "/dev/cdc-wdm0",
		QMIUseProxy:        true,
		QMIProxyPath:       "custom-qmi-proxy",
		QMIProxyExecutable: "/opt/vohive/bin/qmi-proxy",
	})
	if err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}

	devices := readDevicesFromFile(t, path)
	if len(devices) != 1 {
		t.Fatalf("devices=%d want 1", len(devices))
	}
	dev := devices[0]
	if !dev.QMIUseProxy {
		t.Fatal("QMIUseProxy=false, want true")
	}
	if dev.QMIProxyPath != "custom-qmi-proxy" {
		t.Fatalf("QMIProxyPath=%q, want custom-qmi-proxy", dev.QMIProxyPath)
	}
	if dev.QMIProxyExecutable != "/opt/vohive/bin/qmi-proxy" {
		t.Fatalf("QMIProxyExecutable=%q, want /opt/vohive/bin/qmi-proxy", dev.QMIProxyExecutable)
	}
}
