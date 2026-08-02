package backend

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
)

type selectorProbe struct {
	qmi  DeviceBackend
	mbim DeviceBackend
}

func (p selectorProbe) AT(context.Context, device.Candidate) (DeviceBackend, error) { return nil, nil }
func (p selectorProbe) QMI(context.Context, device.Candidate) (DeviceBackend, error) {
	return p.qmi, nil
}
func (p selectorProbe) MBIM(context.Context, device.Candidate) (DeviceBackend, error) {
	return p.mbim, nil
}

func TestSelectDoesNotRequireATPortForQMIOrMBIM(t *testing.T) {
	candidate := device.Candidate{
		Identity:    device.Identity{StableID: "mbim-only"},
		ControlPath: "/dev/cdc-wdm0",
	}
	selection, err := Select(context.Background(), candidate, "mbim", selectorProbe{mbim: &contractLegacy{}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != device.BackendMBIM {
		t.Fatalf("mode=%q", selection.Mode)
	}

	selection, err = Select(context.Background(), candidate, "qmi", selectorProbe{qmi: &contractLegacy{}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != device.BackendQMI {
		t.Fatalf("mode=%q", selection.Mode)
	}
}
