package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateDeviceIMEIInFileWritesOnlyIMEI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  control_device: /dev/cdc-wdm1\n  interface: wwan0\n  at_port: /dev/ttyUSB2\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := UpdateDeviceIMEIInFile(path, map[string]string{"dev1": "990000860099326"}); err != nil {
		t.Fatalf("UpdateDeviceIMEIInFile() error = %v", err)
	}

	got := readDevicesFromFile(t, path)[0]
	if got.ModemIMEI != "990000860099326" {
		t.Fatalf("ModemIMEI = %q, want 990000860099326", got.ModemIMEI)
	}
	// 零路径架构: 读取绝不从文件回填运行时路径字段(mapstructure:"-")。
	if got.ControlDevice != "" || got.Interface != "" || got.ATPort != "" {
		t.Fatalf("runtime path fields must not be loaded from file, got: %+v", got)
	}
}

func TestUpdateDeviceIMEIInFileSkipsEmptyIMEI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  modem_imei: \"123456789012345\"\n  control_device: /dev/cdc-wdm1\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := UpdateDeviceIMEIInFile(path, map[string]string{"dev1": "  "}); err != nil {
		t.Fatalf("UpdateDeviceIMEIInFile() error = %v", err)
	}

	afterCfg := readDevicesFromFile(t, path)
	if len(afterCfg) != 1 || afterCfg[0].ModemIMEI != "123456789012345" {
		t.Fatalf("ModemIMEI was changed or erased: %+v", afterCfg)
	}
}
