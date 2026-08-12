package device

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/snapshot"
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

	mu           sync.RWMutex
	simObserver  SimObserver
	identity     *snapshot.Snapshot[backend.Identity]
	radio        *snapshot.Snapshot[backend.RadioState]
	sim          *snapshot.Snapshot[backend.SIMState]
	currentICCID *snapshot.Snapshot[backend.SIMState]
}

func NewService(runtime *runtime.Runtime) *Service {
	parent := runtime.Context
	return &Service{
		runtime:      runtime,
		identity:     snapshot.New[backend.Identity](snapshot.Policy{Name: "device.identity", TTL: statusSnapshotTTL, LoadTimeout: 30 * time.Second}, parent, nil),
		radio:        snapshot.New[backend.RadioState](snapshot.Policy{Name: "device.radio", TTL: statusSnapshotTTL, LoadTimeout: 30 * time.Second}, parent, nil),
		sim:          snapshot.New[backend.SIMState](snapshot.Policy{Name: "device.sim", TTL: statusSnapshotTTL, LoadTimeout: 30 * time.Second}, parent, nil),
		currentICCID: snapshot.New[backend.SIMState](snapshot.Policy{Name: "device.current_iccid", TTL: time.Minute, LoadTimeout: 30 * time.Second}, parent, nil),
	}
}

const statusSnapshotTTL = 5 * time.Second

// CurrentICCID returns the ICCID from the generation-scoped SIM snapshot. The
// AT path uses AT+QSIMSTAT?/AT+CPIN? for presence, AT+CIMI for IMSI, and
// AT+QCCID for ICCID. It does not perform eSIM EID discovery.
func (s *Service) CurrentICCID(ctx context.Context) string {
	generation := s.runtime.Snapshot().Generation
	sim, _, err := s.currentICCID.Get(ctx, snapshot.Scope{Generation: generation}, func(loadCtx context.Context) (backend.SIMState, error) {
		b, backendErr := s.runtime.Backend()
		if backendErr != nil {
			return backend.SIMState{}, backendErr
		}
		return b.SIM(loadCtx)
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
	runtimeSnapshot := s.runtime.Snapshot()
	out := Status{Snapshot: runtimeSnapshot}
	b, err := s.runtime.Backend()
	if err != nil {
		// Reboot and USB mode changes pass through connecting and initializing
		// states before a backend is ready. The snapshot is the valid status in
		// that window; returning an API error would misreport a normal reconnect.
		if runtimeSnapshot.State != domain.StateReady {
			return out, nil
		}
		return out, err
	}
	out.Identity, _, err = s.identity.Get(ctx, snapshot.Scope{Generation: runtimeSnapshot.Generation}, func(loadCtx context.Context) (backend.Identity, error) {
		return b.Identity(loadCtx)
	})
	if err != nil {
		return out, err
	}
	// 单项能力查询失败(如无 SIM 时 AT 返回 +CME ERROR: 10)不使整个状态失效:
	// 设备身份仍然有效,前端仍可展示已检测到的模组,错误记入 LastError 提示。
	if out.Radio, _, err = s.radio.Get(ctx, snapshot.Scope{Generation: runtimeSnapshot.Generation}, func(loadCtx context.Context) (backend.RadioState, error) {
		return b.Radio(loadCtx)
	}); err != nil {
		out.Snapshot.LastError = err.Error()
	}
	if out.SIM, _, err = s.sim.Get(ctx, snapshot.Scope{Generation: runtimeSnapshot.Generation}, func(loadCtx context.Context) (backend.SIMState, error) {
		return b.SIM(loadCtx)
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

func (s *Service) SnapshotDiagnostics() []snapshot.Summary {
	return []snapshot.Summary{s.identity.Summary(), s.radio.Summary(), s.sim.Summary(), s.currentICCID.Summary()}
}

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
