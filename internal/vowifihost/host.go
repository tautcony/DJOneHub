package vowifihost

import (
	"context"
	"strings"
	"sync"

	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

type State string

const (
	Disabled    State = "disabled"
	Preparing   State = "preparing"
	Connecting  State = "connecting"
	Registering State = "registering"
	Connected   State = "connected"
	Recovering  State = "recovering"
	Failed      State = "failed"
)

type PortFactory interface {
	Open(context.Context) (backend.VoWiFiPort, error)
}

// DependencyChecker is an optional preparation boundary. The host calls it
// before opening a session so identity, SIM/ISIM AKA inputs, network state, and
// packet-tunnel capability are validated outside HTTP handlers.
type DependencyChecker interface {
	CheckVoWiFiDependencies(context.Context) error
}

type Event struct {
	Type string
	Data any
}

type EventSource interface {
	Events(context.Context) (<-chan Event, error)
}

type Host struct {
	mu      sync.RWMutex
	state   State
	port    backend.VoWiFiPort
	factory PortFactory
	bus     *runtime.EventBus
	cancel  context.CancelFunc
	desired bool
	ready   bool
}

func New(factory PortFactory, bus *runtime.EventBus) *Host {
	if bus == nil {
		bus = runtime.NewEventBus()
	}
	return &Host{state: Disabled, factory: factory, bus: bus}
}

func (h *Host) State() State              { h.mu.RLock(); defer h.mu.RUnlock(); return h.state }
func (h *Host) Events() *runtime.EventBus { return h.bus }

func (h *Host) Enable(ctx context.Context) error {
	h.mu.Lock()
	if h.state == Connected {
		h.mu.Unlock()
		return nil
	}
	h.state = Preparing
	h.desired = true
	child, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.mu.Unlock()
	h.publish()
	if h.factory == nil {
		return h.fail(errors.CapabilityMissing(string(domain.CapabilityPacketTunnel), "vowifi_enable", "no packet tunnel factory configured"))
	}
	if checker, ok := h.factory.(DependencyChecker); ok {
		if err := checker.CheckVoWiFiDependencies(child); err != nil {
			return h.fail(err)
		}
	}
	port, err := h.factory.Open(child)
	if err != nil {
		return h.fail(err)
	}
	h.mu.Lock()
	h.port = port
	h.ready = true
	h.state = Connecting
	h.mu.Unlock()
	h.publish()
	if source, ok := port.(EventSource); ok {
		if events, eventErr := source.Events(child); eventErr == nil && events != nil {
			go h.consumeEvents(child, events)
		}
	}
	if err := port.Enable(child); err != nil {
		return h.fail(err)
	}
	h.mu.Lock()
	h.state = Registering
	h.mu.Unlock()
	h.publish()
	h.mu.Lock()
	h.state = Connected
	h.mu.Unlock()
	h.publish()
	return nil
}

func (h *Host) Disable(ctx context.Context) error {
	h.mu.Lock()
	cancel, port := h.cancel, h.port
	h.cancel = nil
	h.port = nil
	h.state = Disabled
	h.desired = false
	h.ready = false
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if port != nil {
		if err := port.Disable(ctx); err != nil {
			h.fail(err)
			return err
		}
	}
	h.publish()
	return nil
}

func (h *Host) Reconnect(ctx context.Context) error {
	h.mu.Lock()
	port := h.port
	h.desired = true
	h.state = Recovering
	h.mu.Unlock()
	h.publish()
	if port == nil {
		return h.Enable(ctx)
	}
	if err := port.Reconnect(ctx); err != nil {
		return h.fail(err)
	}
	h.mu.Lock()
	h.state = Connected
	h.mu.Unlock()
	h.publish()
	return nil
}

// Recover reconnects only when VoWiFi was previously requested by the user.
// Runtime events must not turn a deliberately disabled service back on.
func (h *Host) Recover(ctx context.Context) error {
	h.mu.RLock()
	desired := h.desired
	h.mu.RUnlock()
	if !desired {
		return nil
	}
	return h.Reconnect(ctx)
}

func (h *Host) DeviceRemoved() {
	h.mu.Lock()
	if h.state == Disabled && !h.desired {
		h.mu.Unlock()
		return
	}
	cancel := h.cancel
	port := h.port
	h.cancel = nil
	h.port = nil
	h.ready = false
	h.state = Recovering
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if port != nil {
		_ = port.Disable(context.Background())
	}
	h.publish()
}

func (h *Host) DeviceReady() {
	h.mu.RLock()
	state, desired := h.state, h.desired
	h.mu.RUnlock()
	if state == Recovering && desired {
		h.publish()
		go func() { _ = h.Reconnect(context.Background()) }()
	}
}

func (h *Host) fail(err error) error {
	h.mu.Lock()
	h.state = Failed
	h.mu.Unlock()
	h.publish()
	return err
}

func (h *Host) consumeEvents(ctx context.Context, events <-chan Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			eventType := strings.TrimPrefix(event.Type, "vowifi.")
			h.bus.Publish("vowifi."+eventType, event.Data)
			switch eventType {
			case "device.removed":
				h.DeviceRemoved()
			case "command.expired", "modem.reset", "sim.changed", "network.changed":
				go func() { _ = h.Recover(context.Background()) }()
			}
		}
	}
}

func (h *Host) publish() {
	h.bus.Publish("vowifi.updated", map[string]any{"state": h.State()})
	h.bus.Publish("vowifi.state.changed", map[string]any{"state": h.State()})
}
