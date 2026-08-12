package modem

import (
	"testing"
)

func TestPureControlPlaneBackendMBIM(t *testing.T) {
	if !pureQMIBackendConfig(Config{DeviceBackend: "mbim", ControlDevice: "/dev/cdc-wdm2"}) {
		t.Fatal("mbim 应视为控制面后端，跳过 AT 启动")
	}
	if !pureQMIBackendConfig(Config{DeviceBackend: "mbim"}) {
		t.Fatal("mbim 无 AT 口也应跳过 AT 启动")
	}
	if !pureQMIBackendConfig(Config{DeviceBackend: "qmi"}) {
		t.Fatal("qmi 仍应成立")
	}
	if pureQMIBackendConfig(Config{DeviceBackend: "at", ATPort: "/dev/ttyUSB0"}) {
		t.Fatal("at 不应跳过 AT 启动")
	}
}
