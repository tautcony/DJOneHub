package vowifi

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/transport"
	"github.com/iniwex5/vohive/internal/vowifihost"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

const (
	// vowifiInitialAutoStartDelay 启动后首次期望态自动拉起延迟。
	vowifiInitialAutoStartDelay = 5 * time.Second
	// vowifiDesiredReconcileInterval 期望态对账循环周期。
	vowifiDesiredReconcileInterval = 30 * time.Second
	vowifiDesiredReconcileReason   = "desired_reconcile"
	vowifiInitialAutoStartReason   = "startup_auto"
)

// Config 是 VoWiFi 服务的静态配置（沿用 firmware.ConfigFromEnvironment 惯例）。
type Config struct {
	// Enabled 为 true 时，服务启动后在设备就绪后自动拉起 VoWiFi（期望态）。
	Enabled bool
}

func ConfigFromEnvironment() Config {
	return Config{
		Enabled: os.Getenv("DJONEHUB_VOWIFI_ENABLED") == "1" || strings.EqualFold(os.Getenv("DJONEHUB_VOWIFI_ENABLED"), "true"),
	}
}

// PlatformDependencies 为后续数据面集成预留（TUN 等）；当前会话启动由
// vowifi-go 引擎的 userspace 数据面完成。
type PlatformDependencies struct {
	Network transport.NetworkController
	Tunnel  transport.PacketTunnel
}

// Service 是 VoWiFi 的应用层门面：持有 vowifihost.Manager 及其宿主适配器，
// 负责运行时事件订阅、期望态自动拉起与低频对账。
type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	manager *vowifihost.Manager
	adapter *hostAdapter
	config  Config

	// cardPolicies 是卡片级 VoWiFi 策略（SetStore 注入）；nil 时无策略门控。
	cardPolicies *CardPolicyStore

	// userDesired 记录用户手动启用过 VoWiFi；启用后即使配置未打开，
	// 设备恢复时也会自动拉起（与旧 Host 的 desired 语义一致）。
	userDesiredMu sync.Mutex
	userDesired   bool

	// stopMu guards the cancel/done pair created by Start, following the
	// notification service's Stop pattern so the runtime-event subscription
	// is tied to the session lifecycle.
	stopMu sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewService(devices *device.Service, ops *operation.Manager, rt *runtime.Runtime, platform ...PlatformDependencies) *Service {
	var dependencies PlatformDependencies
	if len(platform) > 0 {
		dependencies = platform[0]
	}
	adapter := newHostAdapter(devices, ops, rt, dependencies)
	manager := vowifihost.NewManager()
	adapter.manager = manager
	manager.ConfigureAdapter(adapter)
	// voice gateway / messaging delivery store / event dispatcher 为后续
	// 集成点（语音呼叫、IMS SMS、事件分发），engine 构造期容忍 nil。
	manager.ConfigureRuntimeDependencies(nil, nil, nil)
	// IMS 注册器：注入后引擎执行真实 SIP REGISTER，State().IMSReady 反映
	// 注册结果（不再走 IMSRegistrar==nil 的占位值）。
	manager.ConfigureIMSRegistrar(buildIMSRegistrar())
	return &Service{
		devices: devices,
		ops:     ops,
		runtime: rt,
		manager: manager,
		adapter: adapter,
		config:  ConfigFromEnvironment(),
	}
}

// SetIMSRegistrar 运行时刷新 IMS 注册器（类似上游 SetVoiceGateway 的
// re-configure 模式）；下次 StartRuntime 起生效。传入 nil 恢复引擎占位行为。
func (s *Service) SetIMSRegistrar(reg runtimehost.IMSRegistrar) {
	s.manager.ConfigureIMSRegistrar(reg)
}

// SwitchBegin / SwitchEnd 暴露给 eSIM 服务的切卡联动（上游
// pool_esim_switch.go 模式）：切卡前抢占拆除 VoWiFi 实例，切卡成功后
// 以 AllowSwitch 恢复。设备槽位固定为 voWiFiDeviceID。
func (s *Service) SwitchBegin(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.SwitchBegin(ctx, voWiFiDeviceID)
}

func (s *Service) SwitchEnd(ctx context.Context, restoreRadio bool) error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.SwitchEnd(ctx, voWiFiDeviceID, restoreRadio)
}

// SetStore 注入持久化层（国家前置代理、卡策略）。应用构造时调用；
// 未注入时相关能力退化为默认行为（直连、无卡策略门控）。
func (s *Service) SetStore(database *storage.SQLiteStore) {
	s.adapter.setStore(database)
	s.cardPolicies = NewCardPolicyStore(database)
}

// Start 订阅运行时事件并启动期望态循环；会话结束时随 runCtx 退出。
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

	s.adapter.setSessionContext(runCtx)
	_, events, unsubscribe := s.runtime.Events().SubscribeNamed("vowifi-runtime", 32)
	stateChanges, unsubState := s.manager.SubscribeState(voWiFiDeviceID)

	go func() {
		defer close(done)
		defer unsubscribe()
		defer unsubState()
		s.followRuntime(runCtx, events, stateChanges)
	}()

	// 期望态：启动延迟后自动拉起 + 低频对账。
	go s.desiredLoops(runCtx)
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

func (s *Service) Running() bool {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	return s.cancel != nil
}

// desiredLoops 运行期望态自动拉起（启动后一次）与 30s 对账循环。
func (s *Service) desiredLoops(ctx context.Context) {
	timer := time.NewTimer(vowifiInitialAutoStartDelay)
	defer timer.Stop()
	ticker := time.NewTicker(vowifiDesiredReconcileInterval)
	defer ticker.Stop()
	initialDone := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-timer.C:
			s.reconcileDesiredVoWiFiOnce(now)
			initialDone = true
		case now := <-ticker.C:
			if initialDone {
				s.reconcileDesiredVoWiFiOnce(now)
			}
		}
	}
}

func (s *Service) followRuntime(ctx context.Context, events <-chan runtime.Event, stateChanges <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-stateChanges:
			s.publishState()
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
					// 模块掉线：拆除旧 VoWiFi 实例，设备就绪后由对账拉起。
					s.manager.TeardownForReconnect(ctx, voWiFiDeviceID)
				case domain.StateReady:
					s.reconcileDesiredVoWiFiOnce(time.Now())
				}
			case "backend.modem.reset", "backend.qmi.modem.reset", "sim.updated", "network.updated":
				s.reconcileDesiredVoWiFiOnce(time.Now())
			}
		}
	}
}

// publishState 将 Manager 的状态变化重发布到 EventBus（前端订阅
// vowifi.updated / vowifi.state.changed，与旧 Host 保持兼容）。
func (s *Service) publishState() {
	deviceID := voWiFiDeviceID
	payload := map[string]any{
		"device_id": deviceID,
		"active":    s.manager.Active(deviceID),
		"starting":  s.manager.Starting(deviceID),
	}
	if state, ok := s.manager.State(deviceID); ok {
		payload["state"] = state
	}
	s.runtime.Events().Publish("vowifi.updated", payload)
	s.runtime.Events().Publish("vowifi.state.changed", payload)
}

// reconcileDesiredVoWiFiOnce 检查期望态：配置或用户手动启用过且设备就绪时，
// 通过退避门控的 ScheduleDesiredRecover 拉起丢失的 VoWiFi 实例。
func (s *Service) reconcileDesiredVoWiFiOnce(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	if !s.shouldReconcileVoWiFi() {
		return
	}
	s.scheduleDesiredVoWiFiRecover(voWiFiDeviceID, vowifiDesiredReconcileReason, now)
}

// shouldReconcileVoWiFi 判断是否允许进入期望态恢复队列。
func (s *Service) shouldReconcileVoWiFi() bool {
	deviceID := voWiFiDeviceID
	if !s.manager.DesiredRecoverable(deviceID) {
		return false
	}
	if !s.config.Enabled && !s.userEnabled() {
		return false
	}
	status, err := s.devices.Status(context.Background())
	if err != nil {
		return false
	}
	iccid := strings.TrimSpace(status.Identity.ICCID)
	imsi := strings.TrimSpace(status.Identity.IMSI)
	if iccid == "" && imsi == "" {
		logger.Warn("VoWiFi 目标态恢复跳过：SIM 身份未就绪", "event", "VOWIFI_DESIRED_RECOVER_SKIPPED_IDENTITY", "device", deviceID)
		return false
	}
	// 卡策略门控：当前卡关闭 VoWiFi 时期望态恢复被跳过并清除退避状态
	// （无策略行视为允许，保持未接线前的行为）。
	if iccid != "" && !s.cardPolicies.AllowsVoWiFi(iccid) {
		s.manager.ClearDesiredRecoverState(deviceID)
		logger.Warn("VoWiFi 目标态恢复跳过：当前卡策略未开启 VoWiFi",
			"event", "VOWIFI_DESIRED_RECOVER_SKIPPED_CARD_POLICY",
			"device", deviceID,
			"iccid", iccid)
		return false
	}
	mcc := strings.TrimSpace(s.adapter.currentMCC())
	if mcc != "" && carrier.IsVoWiFiBlockedMCC(mcc) {
		s.manager.ClearDesiredRecoverState(deviceID)
		logger.Warn("VoWiFi 目标态恢复跳过：MCC 策略禁止", "event", "VOWIFI_DESIRED_RECOVER_SKIPPED_POLICY", "device", deviceID, "mcc", mcc)
		return false
	}
	return true
}

// scheduleDesiredVoWiFiRecover 按设备退避状态排队一次恢复，真正执行仍走
// 生命周期串行控制器。
func (s *Service) scheduleDesiredVoWiFiRecover(deviceID, reason string, now time.Time) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		reason = vowifiDesiredReconcileReason
	}
	if now.IsZero() {
		now = time.Now()
	}
	return s.manager.ScheduleDesiredRecover(s.adapter.Context(), vowifihost.DesiredRecoverRequest{
		DeviceID: deviceID,
		Reason:   reason,
		Now:      now,
		OnResult: s.markDesiredVoWiFiRecoverResult,
	})
}

// markDesiredVoWiFiRecoverResult 根据恢复结果清理状态或安排下一次低频重试。
func (s *Service) markDesiredVoWiFiRecoverResult(deviceID, _ string, err error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	if err == nil {
		s.manager.ClearDesiredRecoverState(deviceID)
		logger.Info("VoWiFi 目标态恢复成功", "event", "VOWIFI_DESIRED_RECOVER_SUCCESS", "device", deviceID)
		return
	}
	if carrier.IsVoWiFiPolicyBlockedError(err) {
		s.manager.ClearDesiredRecoverState(deviceID)
		logger.Warn("VoWiFi 目标态恢复跳过：策略禁止", "event", "VOWIFI_DESIRED_RECOVER_SKIPPED_POLICY", "device", deviceID, "err", err)
		return
	}
	snapshot := s.manager.MarkDesiredRecoverFailed(deviceID, time.Now(), err)
	logger.Warn("VoWiFi 目标态恢复失败，等待低频重试", "event", "VOWIFI_DESIRED_RETRY_DELAY", "device", deviceID, "attempt", snapshot.Attempt, "delay", snapshot.Delay.String(), "err", err)
}

func (s *Service) userEnabled() bool {
	s.userDesiredMu.Lock()
	defer s.userDesiredMu.Unlock()
	return s.userDesired
}

func (s *Service) setUserEnabled(enabled bool) {
	s.userDesiredMu.Lock()
	s.userDesired = enabled
	s.userDesiredMu.Unlock()
}

// ============================================================================
// 公开操作 API（HTTP 层使用；操作在 ResourceVoWiFi 锁下串行执行）
// ============================================================================

func (s *Service) Enable(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.enable", func(taskCtx context.Context) error {
		if err := s.manager.Enable(taskCtx, voWiFiDeviceID); err != nil {
			return err
		}
		s.setUserEnabled(true)
		return nil
	})
}

func (s *Service) Disable(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.disable", func(taskCtx context.Context) error {
		if err := s.manager.Disable(taskCtx, voWiFiDeviceID, "disable", false); err != nil {
			return err
		}
		s.setUserEnabled(false)
		return nil
	})
}

func (s *Service) Reconnect(ctx context.Context) (string, error) {
	return s.run(ctx, "vowifi.reconnect", func(taskCtx context.Context) error {
		return s.manager.Restart(taskCtx, voWiFiDeviceID)
	})
}

func (s *Service) run(ctx context.Context, kind string, task func(context.Context) error) (string, error) {
	return s.ops.Start(ctx, kind, func(taskCtx context.Context, _ string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceVoWiFi)
		if err != nil {
			return err
		}
		defer release()
		progress(10, kind+" started")
		if err := task(taskCtx); err != nil {
			return err
		}
		progress(100, kind+" complete")
		s.ops.Publish("vowifi.updated", map[string]any{"operation": kind})
		return nil
	})
}

// Status 返回 VoWiFi 状态快照（HTTP /api/v1/vowifi 使用）。
func (s *Service) Status(ctx context.Context) (map[string]any, error) {
	deviceID := voWiFiDeviceID
	instances := s.manager.Instances()
	out := map[string]any{
		"device_id": deviceID,
		"available": s.adapter.WorkerExists(deviceID),
		"state":     s.stateString(deviceID),
		"active":    s.manager.Active(deviceID),
		"starting":  s.manager.Starting(deviceID),
		"desired":   s.config.Enabled || s.userEnabled(),
	}
	if inst := instances[deviceID]; inst != nil {
		out["instance"] = inst.Obs()
	}
	if state, ok := s.manager.State(deviceID); ok {
		out["startup_state"] = state
		if strings.TrimSpace(state.LastError) != "" {
			out["last_error"] = state.LastError
		}
		if strings.TrimSpace(state.LastErrorClass) != "" {
			out["error_class"] = state.LastErrorClass
		}
		if strings.TrimSpace(state.LastReason) != "" {
			out["reason"] = state.LastReason
		}
	}
	if snap, ok := s.manager.DesiredRecoverState(deviceID); ok {
		out["desired_recover"] = snap
	}
	return out, nil
}

// stateString 把 Manager 运行态映射为旧 Host 的状态串，保持前端
// vowifi.state 展示契约（disabled/preparing/connecting/registering/
// connected/recovering/failed）。
func (s *Service) stateString(deviceID string) string {
	switch {
	case s.manager.Active(deviceID):
		return "connected"
	case s.manager.Starting(deviceID):
		return "connecting"
	}
	if st, ok := s.manager.State(deviceID); ok {
		switch {
		case st.Phase == runtimehost.PhaseError || strings.TrimSpace(st.LastError) != "":
			return "failed"
		case st.Phase == runtimehost.PhaseStarting:
			return "preparing"
		case st.SIMReady || st.TunnelReady || st.IMSReady:
			return "connecting"
		}
	}
	return "disabled"
}
