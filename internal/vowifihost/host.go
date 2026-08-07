package vowifihost

import (
	"context"
	"strings"
	"sync"
	"time"

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

// recoveryDebounce collapses event-driven recovery triggers arriving within
// the window into one run; a flapping network cannot spawn concurrent
// recovery goroutines.
const recoveryDebounce = 2 * time.Second

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

// Host is the single owner of VoWiFi lifecycle state. All transitions
// (Enable, Disable, Reconnect, Recover, DeviceRemoved, event-driven recovery)
// are serialized through transitionMu so no two transitions can interleave on
// the same port.
type Host struct {
	mu      sync.RWMutex
	state   State
	port    backend.VoWiFiPort
	factory PortFactory
	bus     *runtime.EventBus
	cancel  context.CancelFunc
	desired bool
	ready   bool

	// transitionMu serializes every state transition on the owned port.
	transitionMu sync.Mutex

	// recovery: single-flight and debounced. recoverRunner is installed by
	// the application service so recovery runs under the ResourceVoWiFi lock.
	recoverMu      sync.Mutex
	recoverPending bool
	recoverTimer   *time.Timer
	recoverRunner  func(ctx context.Context) error
	recoverDelay   time.Duration
}

func New(factory PortFactory, bus *runtime.EventBus) *Host {
	if bus == nil {
		bus = runtime.NewEventBus()
	}
	return &Host{state: Disabled, factory: factory, bus: bus, recoverDelay: recoveryDebounce}
}

func (h *Host) State() State              { h.mu.RLock(); defer h.mu.RUnlock(); return h.state }
func (h *Host) Events() *runtime.EventBus { return h.bus }

// SetRecoveryRunner installs the function that executes event-driven
// recovery. The application service sets it so recovery runs under the
// ResourceVoWiFi lock and through the cancellable operation manager; the
// default runs Recover directly for host-level tests.
func (h *Host) SetRecoveryRunner(run func(ctx context.Context) error) {
	h.mu.Lock()
	h.recoverRunner = run
	h.mu.Unlock()
}

func (h *Host) Enable(ctx context.Context) error {
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()
	return h.enableLocked(ctx)
}

func (h *Host) enableLocked(ctx context.Context) error {
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
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()
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
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()
	return h.reconnectLocked(ctx)
}

func (h *Host) reconnectLocked(ctx context.Context) error {
	h.mu.Lock()
	port := h.port
	h.desired = true
	h.state = Recovering
	h.mu.Unlock()
	h.publish()
	if port == nil {
		// The session was torn down (e.g. device removed); a reconnect starts
		// a fresh enable sequence on the same serialized transition.
		return h.enableLocked(ctx)
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
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()
	return h.reconnectLocked(ctx)
}

// TriggerRecovery schedules a single-flight, debounced recovery run. Events
// arriving within the debounce window collapse into the one scheduled run, so
// a flapping network cannot spawn unbounded concurrent recovery goroutines.
func (h *Host) TriggerRecovery() {
	// 未启用过 VoWiFi 时恢复没有意义: 每次模组事件都会启动一个空转的
	// vowifi.recover operation (Recover 同样立即返回), 只在日志中制造噪音。
	h.mu.RLock()
	desired := h.desired
	h.mu.RUnlock()
	if !desired {
		return
	}
	delay := h.recoverDelay
	if delay <= 0 {
		delay = recoveryDebounce
	}
	h.recoverMu.Lock()
	if h.recoverPending {
		h.recoverMu.Unlock()
		return
	}
	h.recoverPending = true
	h.recoverTimer = time.AfterFunc(delay, func() {
		h.recoverMu.Lock()
		h.recoverPending = false
		h.recoverTimer = nil
		h.recoverMu.Unlock()
		h.mu.RLock()
		run := h.recoverRunner
		h.mu.RUnlock()
		if run == nil {
			run = func(ctx context.Context) error { return h.Recover(ctx) }
		}
		_ = run(context.Background())
	})
	h.recoverMu.Unlock()
}

func (h *Host) DeviceRemoved() {
	h.transitionMu.Lock()
	defer h.transitionMu.Unlock()
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
		h.TriggerRecovery()
	}
}

// fail is the single cleanup path for failed transitions: it cancels the
// stored child context, closes any opened port, and clears the tracked state
// before publishing Failed, so repeated failed enables cannot leak modem ports
// or event consumers.
func (h *Host) fail(err error) error {
	h.mu.Lock()
	cancel := h.cancel
	port := h.port
	h.cancel = nil
	h.port = nil
	h.ready = false
	h.state = Failed
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if port != nil {
		_ = port.Disable(context.Background())
	}
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
				h.TriggerRecovery()
			}
		}
	}
}

func (h *Host) publish() {
	h.bus.Publish("vowifi.updated", map[string]any{"state": h.State()})
	h.bus.Publish("vowifi.state.changed", map[string]any{"state": h.State()})
}
