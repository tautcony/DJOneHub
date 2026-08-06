package vowifi

import (
	"context"
	"sync"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/transport"
	"github.com/iniwex5/vohive/internal/vowifihost"
)

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	host    *vowifihost.Host

	// stopMu guards the cancel/done pair created by Start, following the
	// notification service's Stop pattern so the runtime-event subscription
	// is tied to the session lifecycle.
	stopMu sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

type hostPortAdapter struct{ backend.VoWiFiServicePort }

func (p hostPortAdapter) Enable(ctx context.Context) error    { return p.EnableVoWiFi(ctx) }
func (p hostPortAdapter) Disable(ctx context.Context) error   { return p.DisableVoWiFi(ctx) }
func (p hostPortAdapter) Reconnect(ctx context.Context) error { return p.ReconnectVoWiFi(ctx) }
func (p hostPortAdapter) Status(ctx context.Context) (map[string]any, error) {
	return p.VoWiFiStatus(ctx)
}

type PlatformDependencies struct {
	Network transport.NetworkController
	Tunnel  transport.PacketTunnel
}

type hostFactory struct {
	devices  *device.Service
	platform PlatformDependencies
}

func (f hostFactory) Open(ctx context.Context) (backend.VoWiFiPort, error) {
	b, err := f.devices.RequireCapability(domain.CapabilityVoWiFiControl, "vowifi_open")
	if err != nil {
		return nil, err
	}
	if port, ok := b.(backend.VoWiFiPort); ok {
		return port, nil
	}
	if port, ok := b.(backend.VoWiFiServicePort); ok {
		return hostPortAdapter{VoWiFiServicePort: port}, nil
	}
	return nil, backend.AdaptUnsupported("vowifi_control", "vowifi_open")
}

func (f hostFactory) CheckVoWiFiDependencies(ctx context.Context) error {
	b, err := f.devices.RequireCapability(domain.CapabilityVoWiFiControl, "vowifi_enable")
	if err != nil {
		return err
	}
	for _, capability := range []domain.Capability{domain.CapabilitySIM, domain.CapabilityAPDU} {
		if !b.Capabilities(ctx).Has(capability) {
			return backend.AdaptUnsupported(string(capability), "vowifi_enable")
		}
	}
	if f.platform.Network == nil {
		return backend.AdaptUnsupported(string(domain.CapabilityNetworkStatus), "vowifi_enable")
	}
	if f.platform.Tunnel == nil {
		return backend.AdaptUnsupported(string(domain.CapabilityPacketTunnel), "vowifi_enable")
	}
	candidate, err := f.devices.RuntimeCandidate()
	if err != nil {
		return err
	}
	if _, err := f.platform.Network.Status(ctx, candidate); err != nil {
		return err
	}
	identity, err := b.Identity(ctx)
	if err != nil || identity.IMEI == "" {
		if err == nil {
			err = backend.AdaptUnsupported("device_identity", "vowifi_enable")
		}
		return err
	}
	sim, err := b.SIM(ctx)
	if err != nil {
		return err
	}
	if !sim.Inserted {
		return backend.AdaptUnsupported("sim", "vowifi_enable")
	}
	resolver, ok := b.(backend.SIMAuthAIDResolver)
	if !ok {
		return backend.AdaptUnsupported("apdu", "vowifi_sim_auth")
	}
	for _, app := range []struct{ name, fallback string }{
		{name: "usim", fallback: "A0000000871002"},
		{name: "isim", fallback: "A0000000871004"},
	} {
		aid, _, resolveErr := resolver.ResolveSIMAuthAID(ctx, app.name, app.fallback)
		if resolveErr != nil {
			return resolveErr
		}
		if aid == "" {
			return backend.AdaptUnsupported("apdu", "vowifi_sim_auth")
		}
	}
	return nil
}

type legacyPortAdapter struct{ backend.VoWiFiPort }

func (p legacyPortAdapter) EnableVoWiFi(ctx context.Context) error    { return p.Enable(ctx) }
func (p legacyPortAdapter) DisableVoWiFi(ctx context.Context) error   { return p.Disable(ctx) }
func (p legacyPortAdapter) ReconnectVoWiFi(ctx context.Context) error { return p.Reconnect(ctx) }
func (p legacyPortAdapter) VoWiFiStatus(ctx context.Context) (map[string]any, error) {
	return p.Status(ctx)
}

func NewService(devices *device.Service, ops *operation.Manager, rt *runtime.Runtime, platform ...PlatformDependencies) *Service {
	var dependencies PlatformDependencies
	if len(platform) > 0 {
		dependencies = platform[0]
	}
	host := vowifihost.New(hostFactory{devices: devices, platform: dependencies}, rt.Events())
	service := &Service{devices: devices, ops: ops, runtime: rt, host: host}
	// Event-driven recovery runs through the operation manager under the
	// ResourceVoWiFi lock, so it cannot interleave with user Enable/Disable.
	host.SetRecoveryRunner(func(ctx context.Context) error {
		_, err := service.ops.Start(ctx, "vowifi.recover", func(taskCtx context.Context, progress func(int, string)) error {
			release, err := service.runtime.Acquire(taskCtx, runtime.ResourceVoWiFi)
			if err != nil {
				return err
			}
			defer release()
			progress(10, "recovery started")
			return service.host.Recover(taskCtx)
		})
		return err
	})
	return service
}

// Start subscribes to the runtime event bus and ties the subscription to the
// session lifecycle: followRuntime exits when the session context ends and
// Stop joins it before shutdown completes.
func (s *Service) Start(ctx context.Context) {
	s.stopMu.Lock()
	if s.cancel != nil {
		s.stopMu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.stopMu.Unlock()
	_, events, unsubscribe := s.runtime.Events().Subscribe(32)
	go func() {
		defer close(done)
		defer unsubscribe()
		s.followRuntime(runCtx, events)
	}()
}

// Stop cancels the session subscription and waits for followRuntime to exit.
// Repeated calls are safe.
func (s *Service) Stop(ctx context.Context) error {
	s.stopMu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.stopMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) followRuntime(ctx context.Context, events <-chan runtime.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.Type {
			case "device.status.changed":
				snapshot, ok := event.Data.(domain.Snapshot)
				if !ok {
					continue
				}
				switch snapshot.State {
				case domain.StateDisconnected, domain.StateAbsent:
					s.host.DeviceRemoved()
				case domain.StateReady:
					s.host.DeviceReady()
				}
			case "backend.modem.reset", "backend.qmi.modem.reset", "sim.updated", "network.updated":
				// Single-flight, debounced: concurrent triggers collapse into
				// one recovery run under the ResourceVoWiFi lock.
				s.host.TriggerRecovery()
			}
		}
	}
}

func (s *Service) port(name string) (backend.VoWiFiServicePort, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityVoWiFiControl, name)
	if err != nil {
		return nil, err
	}
	port, ok := b.(backend.VoWiFiServicePort)
	if ok {
		return port, nil
	}
	if legacy, ok := b.(backend.VoWiFiPort); ok {
		return legacyPortAdapter{VoWiFiPort: legacy}, nil
	}
	return nil, backend.AdaptUnsupported("vowifi_control", name)
}

func (s *Service) Status(ctx context.Context) (map[string]any, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityVoWiFiInspect, "vowifi_status")
	if err != nil {
		return nil, err
	}
	port, ok := b.(interface {
		VoWiFiStatus(context.Context) (map[string]any, error)
	})
	if ok {
		return port.VoWiFiStatus(ctx)
	}
	if legacy, ok := b.(backend.VoWiFiPort); ok {
		return legacy.Status(ctx)
	}
	return map[string]any{"available": false, "reason": "status_port_not_supported"}, nil
}

func (s *Service) Enable(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.enable", "enable")
}
func (s *Service) Disable(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.disable", "disable")
}
func (s *Service) Reconnect(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.reconnect", "reconnect")
}

func (s *Service) run(ctx context.Context, kind, operationName string) (string, error) {
	if _, err := s.port(operationName); err != nil {
		return "", err
	}
	return s.ops.Start(ctx, kind, func(taskCtx context.Context, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceVoWiFi)
		if err != nil {
			return err
		}
		defer release()
		progress(10, operationName+" started")
		switch operationName {
		case "enable":
			err = s.host.Enable(taskCtx)
		case "disable":
			err = s.host.Disable(taskCtx)
		default:
			err = s.host.Reconnect(taskCtx)
		}
		if err != nil {
			return err
		}
		progress(100, operationName+" complete")
		s.ops.Publish("vowifi.updated", map[string]any{"operation": operationName})
		return nil
	})
}
