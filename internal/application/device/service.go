package device

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

type Status struct {
	Snapshot domain.Snapshot    `json:"snapshot"`
	Identity backend.Identity   `json:"identity"`
	Radio    backend.RadioState `json:"radio"`
	SIM      backend.SIMState   `json:"sim"`
}

type Service struct {
	runtime *runtime.Runtime

	mu             sync.Mutex
	iccidCache     string
	iccidCacheGen  uint64
	simObserver    SimObserver
	iccidCacheTime time.Time
}

func NewService(runtime *runtime.Runtime) *Service { return &Service{runtime: runtime} }

// CurrentICCID returns the ICCID of the inserted SIM, or "" when none is
// available. It is meant for record stamping (SMS, calls) by the 3-second
// pollers, so the modem is only queried when the device generation changed or
// the cache is older than a minute — never on every tick.
func (s *Service) CurrentICCID(ctx context.Context) string {
	generation := s.runtime.Snapshot().Generation
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation == s.iccidCacheGen && !s.iccidCacheTime.IsZero() && time.Since(s.iccidCacheTime) < time.Minute {
		return s.iccidCache
	}
	s.iccidCache = ""
	s.iccidCacheGen = generation
	s.iccidCacheTime = time.Now()
	b, err := s.runtime.Backend()
	if err != nil {
		return ""
	}
	sim, err := b.SIM(ctx)
	if err != nil {
		return ""
	}
	s.iccidCache = strings.TrimSpace(sim.ICCID)
	return s.iccidCache
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	snapshot := s.runtime.Snapshot()
	out := Status{Snapshot: snapshot}
	b, err := s.runtime.Backend()
	if err != nil {
		if snapshot.State == domain.StateAbsent || snapshot.State == domain.StateDisconnected {
			return out, nil
		}
		return out, err
	}
	out.Identity, err = b.Identity(ctx)
	if err != nil {
		return out, err
	}
	// 单项能力查询失败(如无 SIM 时 AT 返回 +CME ERROR: 10)不使整个状态失效:
	// 设备身份仍然有效,前端仍可展示已检测到的模组,错误记入 LastError 提示。
	if out.Radio, err = b.Radio(ctx); err != nil {
		out.Snapshot.LastError = err.Error()
	}
	if out.SIM, err = b.SIM(ctx); err != nil && out.Snapshot.LastError == "" {
		out.Snapshot.LastError = err.Error()
	}
	s.notifySimObserver(out)
	return out, nil
}

// SimObserver 观察每次状态轮询中读到的 SIM 信息（ICCID/IMSI/MSISDN）。
// 由 App 层注入，用于 SIM 卡档案的自动建档与补录；不改变 Status 的行为。
type SimObserver func(sim backend.SIMState, identity backend.Identity)

func (s *Service) SetSimObserver(observer SimObserver) {
	s.simObserver = observer
}

func (s *Service) notifySimObserver(status Status) {
	if s.simObserver == nil {
		return
	}
	iccid := strings.TrimSpace(status.SIM.ICCID)
	if iccid == "" {
		return
	}
	s.simObserver(status.SIM, status.Identity)
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
