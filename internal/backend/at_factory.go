package backend

import (
	"context"
	"fmt"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/modem"
)

// ATFactory creates a business backend for a discovered AT-capable modem.
// The manager is created per runtime connection so a hot-unplug/reconnect
// gets a fresh serial handle and fresh command loops.
type ATFactory struct {
	OpenAT func(context.Context, device.Candidate) (ModemBackend, error)
}

func NewATFactory(openAT func(context.Context, device.Candidate) (ModemBackend, error)) *ATFactory {
	return &ATFactory{OpenAT: openAT}
}

func (f *ATFactory) Open(ctx context.Context, candidate device.Candidate) (ModemBackend, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if candidate.ATPort == "" {
		if f.OpenAT == nil {
			return nil, "", fmt.Errorf("AT port is unavailable for %s", candidate.Identity.StableID)
		}
		b, err := f.OpenAT(ctx, candidate)
		if err != nil {
			return nil, "", err
		}
		return b, "selected platform AT transport", nil
	}

	m, err := modem.New(config.DeviceConfig{
		ID:            candidate.Identity.StableID,
		Name:          candidate.Identity.Product,
		ATPort:        candidate.ATPort,
		ManagePort:    candidate.ATPort,
		DeviceBackend: BackendAT,
		BaudRate:      115200,
		DataBits:      8,
		StopBits:      1,
		Parity:        "none",
		SMSEnabled:    true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("create AT manager: %w", err)
	}
	if err := m.Start(); err != nil {
		_ = m.Close()
		return nil, "", err
	}
	// Initialization commands are asynchronous. Give the manager a bounded
	// window to become usable, while allowing status polling to continue if a
	// modem takes longer than usual to answer.
	_ = m.WaitReady(15 * time.Second)
	return Adapt(NewATBackend(m)), "selected AT serial transport", nil
}
