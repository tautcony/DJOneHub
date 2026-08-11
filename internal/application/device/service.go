package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"golang.org/x/sync/singleflight"
)

type Status struct {
	Snapshot domain.Snapshot    `json:"snapshot"`
	Identity backend.Identity   `json:"identity"`
	Radio    backend.RadioState `json:"radio"`
	SIM      backend.SIMState   `json:"sim"`
}

type Service struct {
	runtime *runtime.Runtime
	ttl     time.Duration

	mu          sync.RWMutex
	simObserver SimObserver
	identity    statusSnapshotCache[backend.Identity]
	radio       statusSnapshotCache[backend.RadioState]
	sim         statusSnapshotCache[backend.SIMState]
}

func NewService(runtime *runtime.Runtime) *Service {
	return &Service{runtime: runtime, ttl: statusSnapshotTTL}
}

const statusSnapshotTTL = 5 * time.Second

// statusSnapshotCache stores one successful value for a runtime generation.
// singleflight coalesces concurrent cold reads without caching errors.
type statusSnapshotCache[T any] struct {
	mu         sync.RWMutex
	value      T
	generation uint64
	loadedAt   time.Time
	valid      bool
	flight     singleflight.Group
}

func (c *statusSnapshotCache[T]) get(ctx context.Context, generation uint64, ttl time.Duration, load func() (T, error)) (T, error) {
	now := time.Now()
	c.mu.RLock()
	if c.valid && c.generation == generation && now.Sub(c.loadedAt) < ttl {
		value := c.value
		c.mu.RUnlock()
		return value, nil
	}
	c.mu.RUnlock()

	value, err, _ := c.flight.Do(fmt.Sprintf("%d", generation), func() (any, error) {
		c.mu.RLock()
		if c.valid && c.generation == generation && time.Since(c.loadedAt) < ttl {
			cached := c.value
			c.mu.RUnlock()
			return cached, nil
		}
		c.mu.RUnlock()
		fresh, loadErr := load()
		if loadErr != nil {
			var zero T
			return zero, loadErr
		}
		c.mu.Lock()
		c.value = fresh
		c.generation = generation
		c.loadedAt = time.Now()
		c.valid = true
		c.mu.Unlock()
		return fresh, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return value.(T), nil
}

// CurrentICCID returns the ICCID from the generation-scoped SIM snapshot. The
// AT path uses AT+QSIMSTAT?/AT+CPIN? for presence, AT+CIMI for IMSI, and
// AT+QCCID for ICCID. It does not perform eSIM EID discovery.
func (s *Service) CurrentICCID(ctx context.Context) string {
	generation := s.runtime.Snapshot().Generation
	b, err := s.runtime.Backend()
	if err != nil {
		return ""
	}
	sim, err := s.sim.get(ctx, generation, time.Minute, func() (backend.SIMState, error) {
		return b.SIM(ctx)
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(sim.ICCID)
}

// Status builds a live device snapshot from Identity, Radio, and SIM. For an
// AT backend this can send AT+CGSN, AT+CIMI, AT+QCCID, AT+CNUM, AT+QGMR with
// AT+CGMR fallback, the registration/operator/radio commands from Radio, and
// the SIM commands AT+QSIMSTAT?/AT+CPIN?, AT+CIMI, and AT+QCCID. Ordinary
// status never sends eSIM discovery commands; EID is read by the eSIM service.
func (s *Service) Status(ctx context.Context) (Status, error) {
	snapshot := s.runtime.Snapshot()
	out := Status{Snapshot: snapshot}
	ttl := s.ttl
	if ttl <= 0 {
		ttl = statusSnapshotTTL
	}
	b, err := s.runtime.Backend()
	if err != nil {
		// Reboot and USB mode changes pass through connecting and initializing
		// states before a backend is ready. The snapshot is the valid status in
		// that window; returning an API error would misreport a normal reconnect.
		if snapshot.State != domain.StateReady {
			return out, nil
		}
		return out, err
	}
	out.Identity, err = s.identity.get(ctx, snapshot.Generation, ttl, func() (backend.Identity, error) {
		return b.Identity(ctx)
	})
	if err != nil {
		return out, err
	}
	// 单项能力查询失败(如无 SIM 时 AT 返回 +CME ERROR: 10)不使整个状态失效:
	// 设备身份仍然有效,前端仍可展示已检测到的模组,错误记入 LastError 提示。
	if out.Radio, err = s.radio.get(ctx, snapshot.Generation, ttl, func() (backend.RadioState, error) {
		return b.Radio(ctx)
	}); err != nil {
		out.Snapshot.LastError = err.Error()
	}
	if out.SIM, err = s.sim.get(ctx, snapshot.Generation, ttl, func() (backend.SIMState, error) {
		return b.SIM(ctx)
	}); err != nil && out.Snapshot.LastError == "" {
		out.Snapshot.LastError = err.Error()
	}
	s.notifySimObserver(out)
	return out, nil
}

// SimObserver 观察每次状态轮询中读到的 SIM 信息（ICCID/IMSI/MSISDN）。
// 由 App 层注入，用于 SIM 卡档案的自动建档与补录；不改变 Status 的行为。
type SimObserver func(sim backend.SIMState, identity backend.Identity)

func (s *Service) SetSimObserver(observer SimObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simObserver = observer
}

func (s *Service) notifySimObserver(status Status) {
	s.mu.RLock()
	observer := s.simObserver
	s.mu.RUnlock()
	if observer == nil {
		return
	}
	iccid := strings.TrimSpace(status.SIM.ICCID)
	if iccid == "" {
		return
	}
	observer(status.SIM, status.Identity)
}

func (s *Service) Rescan(ctx context.Context) error { return s.runtime.Rescan(ctx) }

func (s *Service) Reboot(ctx context.Context) error {
	b, err := s.RequireCapability(domain.CapabilityDeviceControl, "device_reboot")
	if err != nil {
		return err
	}
	release, err := s.runtime.Acquire(ctx, runtime.ResourceDevice)
	if err != nil {
		return err
	}
	defer release()
	rebooter, ok := b.(backend.Rebooter)
	if !ok {
		return derrors.CapabilityMissing(string(domain.CapabilityDeviceControl), "device_reboot", "the selected backend does not expose reset control")
	}
	if err := rebooter.Reboot(ctx); err != nil {
		return err
	}
	s.runtime.Events().Publish("device.rebooted", map[string]any{"accepted": true})
	return nil
}

func (s *Service) RuntimeCandidate() (domain.Candidate, error) { return s.runtime.Candidate() }

func (s *Service) RequireCapability(capability domain.Capability, operation string) (backend.ModemBackend, error) {
	b, err := s.runtime.Backend()
	if err != nil {
		return nil, err
	}
	if !b.Capabilities(context.Background()).Has(capability) {
		return nil, derrors.CapabilityMissing(string(capability), operation, "the selected backend or platform did not advertise this capability")
	}
	return b, nil
}
