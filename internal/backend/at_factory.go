package backend

import (
	"context"
	"fmt"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/modem"
)

// ATFactory creates a business backend for a discovered AT-capable modem.
// The manager is created per runtime connection so a hot-unplug/reconnect
// gets a fresh serial handle and fresh command loops.
type ATFactory struct {
	// OpenTransport opens a platform AT transport for candidates that do not
	// expose an operating-system serial port. The factory owns the transport
	// after it creates the modem manager.
	OpenTransport func(context.Context, device.Candidate) (modem.ATTransport, error)
	// ESIMPort builds an eSIM service port over the manager's shared AT session.
	// The arbiter is created for this runtime connection and is shared by all
	// APDU consumers. A builder error leaves eSIM unavailable without rejecting
	// the device connection.
	ESIMPort func(*modem.Manager, *apduarbiter.Arbiter, device.Candidate) (ESIMPort, error)
}

func NewATFactory(openTransport func(context.Context, device.Candidate) (modem.ATTransport, error)) *ATFactory {
	return &ATFactory{OpenTransport: openTransport}
}

func (f *ATFactory) Open(ctx context.Context, candidate device.Candidate) (ModemBackend, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var m *modem.Manager
	var err error
	if candidate.ATPort == "" {
		if f.OpenTransport == nil {
			return nil, "", fmt.Errorf("AT port is unavailable for %s", candidate.Identity.StableID)
		}
		transport, openErr := f.OpenTransport(ctx, candidate)
		if openErr != nil {
			return nil, "", openErr
		}
		m, err = modem.NewWithATTransport(atManagerConfig(candidate), transport)
	} else {
		m, err = modem.New(atManagerConfig(candidate))
	}
	if err != nil {
		return nil, "", fmt.Errorf("create AT manager: %w", err)
	}
	if err := m.Start(); err != nil {
		_ = m.Close()
		return nil, "", err
	}
	// 设备级 APDU 仲裁器: modem manager 的 AT APDU 透传与 eSIM 服务端口共享
	// 同一实例, 使 SIM 切换 barrier 与 APDU idle 等待覆盖所有 eSIM 路径。
	arbiter := apduarbiter.New(candidate.Identity.StableID, apduarbiter.Options{})
	m.SetAPDUArbiter(arbiter)
	// Initialization commands are asynchronous. Give the manager a bounded
	// window to become usable, while allowing status polling to continue if a
	// modem takes longer than usual to answer.
	_ = m.WaitReady(15 * time.Second)
	at := NewATBackend(m)
	if f.ESIMPort != nil {
		if port, portErr := f.ESIMPort(m, arbiter, candidate); portErr == nil {
			at.SetESIMPort(port)
		}
	}
	if candidate.ATPort == "" {
		return Adapt(at), "selected platform AT transport", nil
	}
	return Adapt(at), "selected AT serial transport", nil
}

func atManagerConfig(candidate device.Candidate) config.DeviceConfig {
	atPort := candidate.ATPort
	if atPort == "" {
		atPort = "injected-at/" + candidate.Identity.StableID
	}
	return config.DeviceConfig{
		ID:            candidate.Identity.StableID,
		Name:          candidate.Identity.Product,
		ATPort:        atPort,
		ManagePort:    candidate.ATPort,
		DeviceBackend: BackendAT,
		BaudRate:      115200,
		DataBits:      8,
		StopBits:      1,
		Parity:        "none",
		SMSEnabled:    true,
	}
}
