package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/domain/device"
)

type Probe interface {
	AT(context.Context, device.Candidate) (DeviceBackend, error)
	QMI(context.Context, device.Candidate) (DeviceBackend, error)
	MBIM(context.Context, device.Candidate) (DeviceBackend, error)
}

type Selection struct {
	Mode    device.BackendMode `json:"mode"`
	Reason  string             `json:"reason"`
	Backend DeviceBackend
}

func Select(ctx context.Context, candidate device.Candidate, preferred string, probe Probe) (Selection, error) {
	if probe == nil {
		return Selection{}, fmt.Errorf("backend probe is required")
	}
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	order := []device.BackendMode{device.BackendQMI, device.BackendMBIM, device.BackendAT}
	if preferred != "" && preferred != "auto" {
		order = []device.BackendMode{device.BackendMode(preferred)}
	}
	var failures []string
	for _, mode := range order {
		var b DeviceBackend
		var err error
		switch mode {
		case device.BackendAT:
			if candidate.ATPort == "" {
				continue
			}
			b, err = probe.AT(ctx, candidate)
		case device.BackendQMI:
			if candidate.ControlPath == "" {
				continue
			}
			b, err = probe.QMI(ctx, candidate)
		case device.BackendMBIM:
			if candidate.ControlPath == "" {
				continue
			}
			b, err = probe.MBIM(ctx, candidate)
		default:
			return Selection{}, fmt.Errorf("unsupported backend mode %q", mode)
		}
		if err == nil && b != nil {
			return Selection{Mode: mode, Reason: fmt.Sprintf("selected %s from available control interface", mode), Backend: b}, nil
		}
		if err != nil {
			failures = append(failures, string(mode)+": "+err.Error())
		}
	}
	return Selection{}, fmt.Errorf("no backend could initialize: %s", strings.Join(failures, "; "))
}
