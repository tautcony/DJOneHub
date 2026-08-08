package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/transport"
)

type Config struct {
	Discovery      transport.DeviceDiscovery
	Backends       backend.BackendFactory
	PollInterval   time.Duration
	ReconnectDelay time.Duration
}

type Runtime struct {
	mu                    sync.RWMutex
	state                 device.State
	snapshot              device.Snapshot
	candidate             *device.Candidate
	backend               backend.ModemBackend
	ctx                   context.Context
	cancel                context.CancelFunc
	workerWG              sync.WaitGroup
	bus                   *EventBus
	locks                 *ResourceLocks
	config                Config
	retryAt               time.Time
	backendEventConsumers int
	// scanMu 是扫描与生命周期关闭共享的串行化锁: 轮询扫描、HTTP rescan、
	// disconnect 与 Stop 全部经过它, 使并发扫描不可能在关闭之后重新安装
	// 一个已关闭的后端 (design D14)。
	scanMu sync.Mutex
}

func New(config Config) (*Runtime, error) {
	if config.Discovery == nil || config.Backends == nil {
		return nil, fmt.Errorf("runtime requires discovery and backend factory")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.ReconnectDelay <= 0 {
		config.ReconnectDelay = time.Second
	}
	return &Runtime{
		state:    device.StateAbsent,
		snapshot: device.Snapshot{State: device.StateAbsent, Capabilities: device.CapabilitySet{}},
		bus:      NewEventBus(), locks: NewResourceLocks(), config: config,
	}, nil
}

func (r *Runtime) Start(parent context.Context) {
	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.workerWG.Add(1)
	r.mu.Unlock()
	go func() { defer r.workerWG.Done(); r.loop(r.ctx) }()
}

func (r *Runtime) Stop() {
	// 与扫描互斥: 取走后端并取消上下文后再等待 worker。注意不能在持有
	// scanMu 时等待 workerWG — 轮询循环的下一次 scan 需要 scanMu, 会死锁。
	// 取消上下文后, 迟到的 scan 由 scanLocked 的运行时上下文检查拒绝。
	r.scanMu.Lock()
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	b := r.backend
	r.backend = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if b != nil {
		_ = b.Close()
	}
	r.scanMu.Unlock()
	r.workerWG.Wait()
}

func (r *Runtime) Snapshot() device.Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := r.snapshot
	out.Capabilities = r.snapshot.Capabilities.Clone()
	return out
}

func (r *Runtime) Events() *EventBus     { return r.bus }
func (r *Runtime) Locks() *ResourceLocks { return r.locks }

func (r *Runtime) Rescan(ctx context.Context) error {
	return r.scan(ctx)
}

func (r *Runtime) loop(ctx context.Context) {
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	_ = r.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.scan(ctx)
		}
	}
}

// scan 是轮询循环与 HTTP rescan 共用的唯一扫描入口, 经 scanMu 串行化;
// 并发扫描不会相互交错, 也不会与生命周期关闭交错。
func (r *Runtime) scan(ctx context.Context) error {
	r.scanMu.Lock()
	defer r.scanMu.Unlock()
	return r.scanLocked(ctx)
}

func (r *Runtime) scanLocked(ctx context.Context) error {
	// 已停止的运行时拒绝扫描: 一个在关闭后到达的 HTTP rescan 不会重新安装
	// 已关闭的后端。未启动 (ctx 为空, 测试路径) 时照常运行。
	r.mu.RLock()
	runtimeCtx := r.ctx
	r.mu.RUnlock()
	if runtimeCtx != nil && runtimeCtx.Err() != nil {
		return derrors.New(derrors.Unavailable, "runtime is stopped", true, nil)
	}
	candidates, err := r.config.Discovery.Discover(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		r.disconnectLocked(derrors.New(derrors.DeviceOffline, "no managed device was discovered", true, nil))
		return nil
	}
	candidate := candidates[0]
	r.mu.RLock()
	currentID := r.snapshot.Identity.StableID
	currentState := r.state
	r.mu.RUnlock()
	if currentState == device.StateReady || currentState == device.StateInitializing || currentState == device.StateConnecting {
		if currentID == candidate.StableID() {
			r.mu.Lock()
			r.candidate = &candidate
			r.mu.Unlock()
			return nil
		}
		r.disconnectLocked(derrors.New(derrors.DeviceOffline, "managed device was re-enumerated", true, nil))
	}
	if currentState == device.StateDegraded && currentID != candidate.StableID() {
		r.disconnectLocked(derrors.New(derrors.DeviceOffline, "managed device identity changed", true, nil))
	}
	if currentState == device.StateDegraded && currentID == candidate.StableID() {
		r.mu.RLock()
		retryAt := r.retryAt
		r.mu.RUnlock()
		if time.Now().Before(retryAt) {
			return nil
		}
		if err := r.transition(device.StateConnecting, candidate.Identity, "retrying backend initialization"); err != nil {
			return err
		}
	} else {
		if err := r.transition(device.StateDiscovered, candidate.Identity, ""); err != nil {
			return err
		}
		if err := r.transition(device.StateConnecting, candidate.Identity, ""); err != nil {
			return err
		}
	}
	b, reason, err := r.config.Backends.Open(ctx, candidate)
	if err != nil {
		r.mu.Lock()
		r.retryAt = time.Now().Add(r.config.ReconnectDelay)
		r.mu.Unlock()
		_ = r.transition(device.StateDegraded, candidate.Identity, err.Error())
		return err
	}
	r.mu.Lock()
	r.candidate = &candidate
	r.backend = b
	r.mu.Unlock()
	if err := r.transition(device.StateInitializing, candidate.Identity, ""); err != nil {
		_ = b.Close()
		return err
	}
	caps := b.Capabilities(ctx).Clone()
	identity, err := b.Identity(ctx)
	if err != nil {
		_ = b.Close()
		r.mu.Lock()
		r.backend = nil
		r.mu.Unlock()
		_ = r.transition(device.StateDegraded, candidate.Identity, err.Error())
		return err
	}
	if identity.IMEI != "" {
		candidate.Identity.IMEI = identity.IMEI
	}
	if identity.ICCID != "" {
		if candidate.Metadata == nil {
			candidate.Metadata = make(map[string]string)
		}
		candidate.Metadata["iccid"] = identity.ICCID
	}
	_ = r.transition(device.StateReady, candidate.Identity, "")
	r.mu.Lock()
	r.retryAt = time.Time{}
	r.snapshot.Backend = device.BackendMode(b.Mode())
	r.snapshot.BackendReason = reason
	r.snapshot.Capabilities = caps
	r.snapshot.Identity = candidate.Identity
	r.snapshot.LastError = ""
	r.snapshot.Generation++
	snapshot := r.snapshot
	r.mu.Unlock()
	r.bus.Publish("device.status.changed", snapshot)
	if events, eventErr := b.Events(ctx); eventErr == nil && events != nil {
		go r.consumeBackendEvents(ctx, b, events)
	}
	return nil
}

func (r *Runtime) consumeBackendEvents(ctx context.Context, b backend.ModemBackend, events <-chan backend.BackendEvent) {
	r.mu.Lock()
	r.backendEventConsumers++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.backendEventConsumers--
		r.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			r.bus.Publish("backend."+event.Type, event.Data)
			switch {
			case event.Type == "sim.changed":
				r.bus.Publish("sim.updated", event.Data)
			case event.Type == "sms.received" || event.Type == "sms.changed":
				r.bus.Publish("sms.updated", event.Data)
			case event.Type == "esim.changed":
				r.bus.Publish("esim.updated", event.Data)
			case event.Type == "network.changed":
				r.bus.Publish("network.updated", event.Data)
			case event.Type == "vowifi.changed":
				r.bus.Publish("vowifi.updated", event.Data)
			}
			if event.Type == "device.removed" || event.Type == "transport.closed" {
				r.disconnect(derrors.New(derrors.DeviceOffline, "backend reported device removal", true, nil))
				return
			}
		}
	}
}

type Diagnostics struct {
	Running               bool         `json:"running"`
	State                 device.State `json:"state"`
	PollIntervalMS        int64        `json:"poll_interval_ms"`
	BackendAttached       bool         `json:"backend_attached"`
	BackendEventConsumers int          `json:"backend_event_consumers"`
}

func (r *Runtime) Diagnostics() Diagnostics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Diagnostics{
		Running: r.cancel != nil, State: r.state,
		PollIntervalMS:  r.config.PollInterval.Milliseconds(),
		BackendAttached: r.backend != nil, BackendEventConsumers: r.backendEventConsumers,
	}
}

func (r *Runtime) transition(next device.State, identity device.Identity, lastError string) error {
	r.mu.Lock()
	previous := r.state
	state, err := device.Transition(previous, next)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.state = state
	r.snapshot.State = state
	r.snapshot.Identity = identity
	r.snapshot.LastError = lastError
	r.snapshot.Generation++
	snapshot := r.snapshot
	r.mu.Unlock()
	if previous != next {
		r.bus.Publish("device.status.changed", snapshot)
		if isOfflineState(state) {
			r.bus.Publish("device.offline", device.OfflineEvent{State: state, LastError: lastError, Reason: lastError})
		}
	}
	return nil
}

func isOfflineState(state device.State) bool {
	switch state {
	case device.StateDegraded, device.StateDisconnected, device.StateAbsent:
		return true
	}
	return false
}

// disconnect 由后端事件循环等扫描外部路径调用: 与扫描互斥后执行断开。
func (r *Runtime) disconnect(reason error) {
	r.scanMu.Lock()
	defer r.scanMu.Unlock()
	r.disconnectLocked(reason)
}

// disconnectLocked 假定调用方已持有 scanMu (扫描内部路径直接调用)。
func (r *Runtime) disconnectLocked(reason error) {
	r.mu.Lock()
	b := r.backend
	r.backend = nil
	state := r.state
	r.mu.Unlock()
	if b != nil {
		_ = b.Close()
	}
	if state == device.StateReady || state == device.StateDegraded || state == device.StateInitializing || state == device.StateConnecting {
		_ = r.transition(device.StateDisconnected, device.Identity{}, reason.Error())
	}
	if state == device.StateDisconnected {
		return
	}
	if state == device.StateAbsent {
		return
	}
	_ = r.transition(device.StateAbsent, device.Identity{}, reason.Error())
}

func (r *Runtime) Backend() (backend.ModemBackend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.backend == nil || r.state != device.StateReady {
		return nil, derrors.New(derrors.DeviceOffline, "device is not ready", true, nil)
	}
	return r.backend, nil
}

func (r *Runtime) Candidate() (device.Candidate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.candidate == nil {
		return device.Candidate{}, derrors.New(derrors.DeviceOffline, "device is not discovered", true, nil)
	}
	return *r.candidate, nil
}

func (r *Runtime) Acquire(ctx context.Context, resource Resource) (func(), error) {
	return r.locks.Acquire(ctx, resource)
}
