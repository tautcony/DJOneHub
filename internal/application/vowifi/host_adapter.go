package vowifi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/runtime"
	innersim "github.com/iniwex5/vohive/internal/sim"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/upstreamproxy"
	"github.com/iniwex5/vohive/internal/vowifihost"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// voWiFiDeviceID 是 VoWiFi 生命周期管理器使用的固定设备槽位。
// DJOneHub 是单设备运行时，manager 按 deviceID 分槽，固定 ID 保证
// 插拔/重建后状态连续。
const voWiFiDeviceID = "main"

// hostAdapter 实现 vowifihost.Adapter：把 DJOneHub 的单设备运行时
// （runtime.Runtime + backend 后端 + operation manager）适配为
// vowifihost.Manager 的宿主。移植自 vohive-open internal/device 的
// pool_vowifi_* 系列（Adapter 实现部分）。
type hostAdapter struct {
	devices  *device.Service
	ops      *operation.Manager
	runtime  *runtime.Runtime
	platform PlatformDependencies
	// store 是持久化层（国家前置代理/卡策略用）；Service.SetStore 注入，nil 时
	// 相关能力退化为默认行为（直连、无策略门控）。
	store *storage.SQLiteStore

	manager *vowifihost.Manager

	mu  sync.RWMutex
	ctx context.Context
}

func newHostAdapter(devices *device.Service, ops *operation.Manager, rt *runtime.Runtime, platform PlatformDependencies) *hostAdapter {
	return &hostAdapter{
		devices:  devices,
		ops:      ops,
		runtime:  rt,
		platform: platform,
		ctx:      context.Background(),
	}
}

// setStore 由 Service.SetStore 注入持久化层。
func (a *hostAdapter) setStore(store *storage.SQLiteStore) {
	a.store = store
}

// setSessionContext 由 Service.Start 注入会话上下文。
func (a *hostAdapter) setSessionContext(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx != nil {
		a.ctx = ctx
	}
}

func (a *hostAdapter) Context() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// IsSwitching 报告是否有进行中的 eSIM 切卡操作（esim.enable/esim.disable）。
// 联动上游 internal/device/pool_esim_switch.go 的 SwitchBegin/SwitchEnd：
// 切卡期间 enableRuntime 的 IsSwitching 拒绝门生效，防止启用中的 VoWiFi
// 实例与切卡冲突（本仓库新增接线，非上游代码）。
func (a *hostAdapter) IsSwitching(deviceID string) bool {
	if a.ops == nil {
		return false
	}
	return a.ops.HasActiveKind("esim.enable", "esim.disable")
}

// WorkerExists 设备存在 = 运行时后端已打开。
func (a *hostAdapter) WorkerExists(deviceID string) bool {
	_, err := a.runtime.Backend()
	return err == nil
}

// WaitQMICoreReady 等待 SIM/身份就绪：后端可执行 APDU、SIM 已插入、
// IMEI 可读（对应上游 UIMReadiness / QMICore.WaitIdentityReady）。
func (a *hostAdapter) WaitQMICoreReady(deviceID string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(a.Context(), timeout)
	defer cancel()
	return waitForCondition(waitCtx, 200*time.Millisecond, func() bool {
		b, err := a.devices.RequireCapability(domain.CapabilityAPDU, "vowifi_wait_qmi_core")
		if err != nil {
			return false
		}
		sim, simErr := b.SIM(waitCtx)
		if simErr != nil || !sim.Inserted {
			return false
		}
		identity, identityErr := b.Identity(waitCtx)
		if identityErr != nil || strings.TrimSpace(identity.IMEI) == "" {
			return false
		}
		return true
	})
}

// WaitWorkerReady 等待设备运行态就绪（对应上游 worker.IsDeviceHealthy）。
func (a *hostAdapter) WaitWorkerReady(deviceID string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(a.Context(), timeout)
	defer cancel()
	return waitForCondition(waitCtx, 200*time.Millisecond, func() bool {
		state := a.runtime.Snapshot().State
		return state == domain.StateReady || state == domain.StateDegraded
	})
}

// PrepareStart 启动前准备：构建身份画像、SIM AKA provider、进入飞行模式。
func (a *hostAdapter) PrepareStart(deviceID, traceID, runtimeEPDGOverride string) (vowifihost.PreparedStart, error) {
	startCtx := a.Context()
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		deviceID = voWiFiDeviceID
	}

	b, err := a.devices.RequireCapability(domain.CapabilityAPDU, "vowifi_prepare_start")
	if err != nil {
		return vowifihost.PreparedStart{}, err
	}
	db, m := unwrapLegacyBackend(b)
	if db == nil && m == nil {
		return vowifihost.PreparedStart{}, fmt.Errorf("后端 %s 不支持 VoWiFi 所需的 SIM/APDU 能力", b.Mode())
	}

	modemIface, errModemIface := newVoWiFiModemInterface(db, m, deviceID)
	if errModemIface != nil {
		return vowifihost.PreparedStart{}, errModemIface
	}
	if _, ok := modemIface.(*qmiModemAdapter); ok {
		if strings.EqualFold(backendBackendMode(db), backend.BackendAT) {
			logger.Info("VoWiFi 使用 USB AT 逻辑通道鉴权", "trace_id", traceID, "device", deviceID)
		} else {
			logger.Info("VoWiFi 使用 QMI/MBIM 模式鉴权", "trace_id", traceID, "device", deviceID)
		}
	}

	status := voWiFiDeviceStatus(db, m, startCtx)
	startProfile, errProfile := buildVoWiFiStartProfile(db, m, status, traceID)
	if errProfile != nil {
		logger.Error("构建 VoWiFi 启动画像失败", "trace_id", traceID, "device", deviceID, "err", errProfile)
		return vowifihost.PreparedStart{}, errProfile
	}

	akaProvider := innersim.BuildAKAProvider(akaProviderInput{
		db:       db,
		modem:    modemIface,
		deviceID: deviceID,
	})
	if akaProvider == nil {
		if strings.EqualFold(backendBackendMode(db), backend.BackendMBIM) {
			return vowifihost.PreparedStart{}, fmt.Errorf("设备 %s 的 MBIM 不支持 AKA(AUTH 与逻辑通道均不可用),如需 VoWiFi 请切 QMI 组态", deviceID)
		}
		return vowifihost.PreparedStart{}, fmt.Errorf("设备 %s 无可用 AKA provider", deviceID)
	}
	if strings.EqualFold(backendBackendMode(db), backend.BackendMBIM) {
		logger.Info("VoWiFi 使用 MBIM Auth(AKA) 鉴权", "trace_id", traceID, "device", deviceID)
	} else {
		logger.Info("VoWiFi 使用 APDU(AKA) 鉴权", "trace_id", traceID, "device", deviceID)
	}

	if carrier.IsVoWiFiBlockedMCC(startProfile.MCC) {
		err := carrier.NewVoWiFiBlockedMCCError(startProfile.MCC)
		logger.Warn("VoWiFi 启动被运营商策略拦截",
			"trace_id", traceID,
			"device", deviceID,
			"mcc", formatVoWiFiPLMN3(startProfile.MCC),
			"imsi", startProfile.IMSI,
			"err", err)
		return vowifihost.PreparedStart{}, err
	}

	runtimehost.SetLogger(logger.ZapLogger())
	prepared, errPrepare := identity.PrepareStart(identity.PrepareStartInput{
		DeviceID:            deviceID,
		Profile:             startProfile,
		RuntimeEPDGOverride: runtimeEPDGOverride,
		Access:              runtimehost.NewModemAccessAdapter(modemIface),
	})
	if errPrepare != nil {
		logger.Warn("VoWiFi 启动画像准备失败",
			"trace_id", traceID,
			"device", deviceID,
			"err", errPrepare)
		return vowifihost.PreparedStart{}, errPrepare
	}
	logger.Info("VoWiFi 启动画像已准备",
		"trace_id", traceID,
		"device", deviceID,
		"matched_plmn", prepared.EffectiveCarrier.MCC+"/"+prepared.EffectiveCarrier.MNC,
		"preset_id", prepared.EffectiveCarrier.PresetID,
		"epdg_source", prepared.EPDGSource,
		"epdg", prepared.EPDGAddr,
		"identity_source", prepared.IdentityIMEISource)

	// 进入飞行模式以禁用原生 IMS 注册。切卡恢复场景下设备可能已处于
	// 飞行模式，此时无需再次切换（冗余的 SetOperatingMode(LowPower) 会触发
	// 模组内部 UIM Session Close，导致后续 AKA 认证失败）。
	if db != nil {
		alreadyInFlight := false
		if opMode, opErr := db.GetOperatingMode(startCtx); opErr == nil {
			alreadyInFlight = isFlightOperatingMode(opMode)
		}
		if strings.EqualFold(db.Mode(), backend.BackendMBIM) {
			logger.Info("MBIM 后端不支持真正的低功耗模式",
				"trace_id", traceID, "device", deviceID)
		} else if alreadyInFlight {
			logger.Info("设备已处于飞行模式，跳过冗余的飞行模式切换",
				"trace_id", traceID, "device", deviceID, "backend", db.Mode())
		} else {
			logger.Info("进入飞行模式以禁用原生 IMS 注册",
				"trace_id", traceID, "device", deviceID, "backend", db.Mode())
			if err := db.SetOperatingMode(startCtx, backend.ModeRFOff); err != nil {
				logger.Warn("进入飞行模式失败，继续尝试建立隧道",
					"trace_id", traceID, "device", deviceID, "err", err)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	networkMode := modemIface.GetNetworkMode()
	return vowifihost.PreparedStart{
		Profile:      startProfile,
		Prepared:     prepared,
		Modem:        modemIface,
		SIM:          runtimehost.NewReaderSIMAdapter(akaProvider),
		Proxy:        a.resolveVoWiFiCountryProxy(startProfile.MCC, traceID, deviceID),
		NetworkMode:  networkMode,
		StartupState: newVoWiFiSIMReadyStartupState(deviceID, swu.DataplaneModeUserspace, networkMode, time.Now()),
	}, nil
}

// BeforeStart 返回 session 启动前的钩子：记录启动态、注册态，并对命中的
// 国家前置代理执行 SOCKS5 自检（失败即拒绝启动，避免隧道建在不可达代理上）。
func (a *hostAdapter) BeforeStart(deviceID string, modemIface runtimehost.Modem, proxy *runtimehost.ProxyConfig) func(context.Context, runtimehost.SessionConfig) error {
	return func(startCtx context.Context, cfg runtimehost.SessionConfig) error {
		startupState := newVoWiFiSIMReadyStartupState(deviceID, cfg.DataplaneMode, modemIface.GetNetworkMode(), time.Now())
		startupState.RegStatus, startupState.RegStatusText = modemIface.GetRegStatus()
		if proxy != nil && proxy.Enabled && strings.TrimSpace(proxy.Addr) != "" {
			probeRes, probeErr := upstreamproxy.ProbeSOCKS5(startCtx, upstreamproxy.ProbeConfig{
				ProxyAddr: proxy.Addr,
				Username:  proxy.Username,
				Password:  proxy.Password,
				Timeout:   5 * time.Second,
			})
			if probeErr != nil {
				startupState.LastErrorClass = "proxy"
				startupState.LastError = probeErr.Error()
				startupState.LastReason = probeRes.FailureSummary()
				if a.manager != nil {
					a.manager.RecordStartupState(deviceID, startupState)
				}
				return fmt.Errorf("前置代理自检失败: %w", probeErr)
			}
			logger.Info("VoWiFi 前置代理自检通过",
				"trace_id", cfg.TraceID,
				"device", deviceID,
				"proxy_addr", proxy.Addr,
				"stage", probeRes.Stage,
				"duration_ms", probeRes.DurationMS)
		}
		if a.manager != nil {
			a.manager.RecordStartupState(deviceID, startupState)
		}
		return nil
	}
}

// resolveVoWiFiCountryProxy 由 SIM home MCC 解析国家前置代理
// （移植自 vohive-open internal/device/vowifi_start_orchestrator.go
// resolveVoWiFiCountryProxy）。store 未注入或未命中时返回 nil（直连）。
func (a *hostAdapter) resolveVoWiFiCountryProxy(homeMCC, traceID, deviceID string) *runtimehost.ProxyConfig {
	homeMCC = strings.TrimSpace(homeMCC)
	if a.store == nil || homeMCC == "" {
		return nil
	}
	proxy, countryCode, err := a.store.GetHomeMCCUpstreamProxy(homeMCC)
	if err != nil {
		logger.Warn("VoWiFi 启动前读取国家前置代理配置失败",
			"trace_id", traceID,
			"device", deviceID,
			"home_mcc", homeMCC,
			"err", err)
		return nil
	}
	if proxy == nil {
		logger.Info("VoWiFi 国家前置代理未命中，使用直连",
			"trace_id", traceID,
			"device", deviceID,
			"home_mcc", homeMCC,
			"proxy_country_code", countryCode,
			"mcc_table_ready", upstreamproxy.CountryTableReady(),
			"proxy_route", "direct")
		return nil
	}
	logger.Info("VoWiFi 国家前置代理已命中",
		"trace_id", traceID,
		"device", deviceID,
		"home_mcc", homeMCC,
		"proxy_country_code", countryCode,
		"upstream_proxy_id", proxy.ID,
		"proxy_route", "country_rule")
	return &runtimehost.ProxyConfig{
		ID:       proxy.ID,
		Addr:     proxy.Addr,
		Username: proxy.Username,
		Password: proxy.Password,
		Enabled:  proxy.Enabled,
	}
}

// HandleStartupError 启动失败处理：APDU busy 走 3/5/10s 短退避自动恢复，
// 其余错误记录汇总并恢复射频。
func (a *hostAdapter) HandleStartupError(req vowifihost.StartupErrorRequest) error {
	if req.Err != nil && strings.Contains(strings.ToLower(req.Err.Error()), "apdu busy") {
		logger.Debug("VoWiFi 启动遇到 APDU busy，等待短退避恢复",
			"trace_id", req.TraceID,
			"device", req.DeviceID,
			"err", req.Err)
		a.scheduleVoWiFiAPDUBusyRecover(req.DeviceID, req.RuntimeEPDGOverride, req.Generation)
		a.restoreRadioAfterVoWiFiStartupFailure(req.TraceID, req.DeviceID)
		return req.Err
	}

	logger.Error("VoWiFi 启动失败", "trace_id", req.TraceID, "device", req.DeviceID, "err", req.Err)
	retryable := shouldRetryVoWiFiAutoStart(req.Err)
	nextRetry := vowifihost.DesiredRecoverDelay(0)
	if !retryable {
		nextRetry = 0
	}
	logVoWiFiFailureSummary(req.TraceID, req.DeviceID, "startup", req.State.LastErrorClass, req.Err.Error(), retryable, nextRetry)
	a.restoreRadioAfterVoWiFiStartupFailure(req.TraceID, req.DeviceID)
	return req.Err
}

// MarkRuntimeStarted 运行时启动完成：VoWiFi 会话期间暂停 AT 短信 URC 读取，
// 避免与隧道/SIM 通道争用。
func (a *hostAdapter) MarkRuntimeStarted(req vowifihost.RuntimeStartedRequest) {
	if m := a.atModem(); m != nil {
		m.SetDisableURCRead(true)
	}
	logger.Info("VoWiFi 已启用、短信模式已切换为 VoWiFi", "trace_id", req.TraceID, "device", req.DeviceID, "active_count", req.ActiveCount)
}

// RestoreSMSMode 恢复 AT 短信 URC 读取（CNMI 重新使能）。
func (a *hostAdapter) RestoreSMSMode(deviceID string) {
	m := a.atModem()
	if m == nil {
		return
	}
	m.SetDisableURCRead(false)
	_, _ = m.ExecuteATSilent("AT+CNMI=2,1,0,0,0", 2*time.Second)
	logger.Info("短信模式已恢复", "device", deviceID)
}

// RestoreRadioAfterVoWiFi 退出飞行模式恢复射频，并等待关键路径就绪。
func (a *hostAdapter) RestoreRadioAfterVoWiFi(deviceID string) error {
	db, _ := a.currentBackend()
	if db == nil {
		return nil
	}
	logger.Info("退出飞行模式恢复射频", "device", deviceID)
	if err := db.SetOperatingMode(a.Context(), backend.ModeOnline); err != nil {
		return err
	}
	logger.Info("等待射频及基带完全启动重获端口控制权...", "device", deviceID)
	waitCtx, cancel := context.WithTimeout(a.Context(), 5*time.Second)
	defer cancel()
	if err := waitForCondition(waitCtx, 200*time.Millisecond, func() bool {
		state := a.runtime.Snapshot().State
		return state == domain.StateReady || state == domain.StateDegraded
	}); err != nil {
		logger.Warn("等待射频恢复关键路径就绪超时", "device", deviceID, "err", err)
	}
	return nil
}

// ============================================================================
// 内部辅助（移植自 vohive-open internal/device）
// ============================================================================

func waitForCondition(ctx context.Context, interval time.Duration, check func() bool) error {
	if check() {
		return nil
	}
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}

// unwrapLegacyBackend 从业务后端取出协议后端（DeviceBackend）与 AT 路径的
// modem.Manager。BusinessAdapter 通过 Legacy() 暴露被包装的 DeviceBackend；
// 其他直接实现 VoWiFi 窄接口的后端也可以直接使用。
func unwrapLegacyBackend(b backend.ModemBackend) (voWiFiBackend, *modem.Manager) {
	if b == nil {
		return nil, nil
	}
	// ModemBackend 与 DeviceBackend 的 ListSMS 签名不同，不可能同型；
	// 只有 BusinessAdapter（暴露 Legacy()）能解开协议后端。
	if legacyProvider, ok := b.(interface{ Legacy() backend.DeviceBackend }); ok {
		db := legacyProvider.Legacy()
		if at, ok := db.(*backend.ATBackend); ok {
			return at, at.Modem()
		}
		return db, nil
	}
	if direct, ok := b.(voWiFiBackend); ok {
		return direct, nil
	}
	return nil, nil
}

// currentBackend 取当前运行时后端的协议后端与 AT modem（仅用于 teardown 恢复路径）。
func (a *hostAdapter) currentBackend() (voWiFiBackend, *modem.Manager) {
	b, err := a.runtime.Backend()
	if err != nil {
		return nil, nil
	}
	return unwrapLegacyBackend(b)
}

func (a *hostAdapter) atModem() *modem.Manager {
	_, m := a.currentBackend()
	return m
}

func backendBackendMode(db voWiFiBackend) string {
	if db == nil {
		return ""
	}
	return db.Mode()
}

// voWiFiDeviceStatus 构建启动画像所需的设备状态：AT 路径用 modem.DeviceStatus
// （含已缓存 NativeMCC/MNC），QMI/MBIM 路径从后端 Identity 组装最小状态。
func voWiFiDeviceStatus(db voWiFiBackend, m *modem.Manager, ctx context.Context) *modem.DeviceStatus {
	if m != nil {
		status := m.GetFullStatus()
		return &status
	}
	status := &modem.DeviceStatus{}
	if db != nil {
		liveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if v, err := db.GetIMEI(liveCtx); err == nil {
			status.IMEI = v
		}
		if v, err := db.GetIMSI(liveCtx); err == nil {
			status.IMSI = v
		}
		if v, err := db.GetICCID(liveCtx); err == nil {
			status.ICCID = v
		}
	}
	return status
}

// currentMCC 读取当前 SIM 归属 MCC（期望态对账的 MCC 策略预检查用）。
func (a *hostAdapter) currentMCC() string {
	db, m := a.currentBackend()
	if m != nil {
		if mcc := strings.TrimSpace(m.GetFullStatus().NativeMCC); mcc != "" {
			return mcc
		}
	}
	if db != nil {
		ctx, cancel := context.WithTimeout(a.Context(), 3*time.Second)
		defer cancel()
		if mcc, _, err := db.GetNativeMCCMNC(ctx); err == nil {
			return strings.TrimSpace(mcc)
		}
	}
	return ""
}

func isFlightOperatingMode(mode backend.OperatingMode) bool {
	return mode == backend.ModeRFOff || mode == backend.ModeLowPower
}

func formatVoWiFiPLMN3(mcc string) string {
	mcc = strings.TrimSpace(mcc)
	if len(mcc) != 3 {
		return mcc
	}
	return mcc
}

func logVoWiFiFailureSummary(traceID, deviceID, stage, errorClass, reason string, retryable bool, nextRetry time.Duration) {
	if strings.TrimSpace(errorClass) == "" {
		errorClass = "unknown"
	}
	logger.Warn("VoWiFi 失败汇总",
		"trace_id", traceID,
		"device", deviceID,
		"stage", stage,
		"error_class", errorClass,
		"reason", reason,
		"retryable", retryable,
		"next_retry", nextRetry.String())
}

func shouldRetryVoWiFiAutoStart(err error) bool {
	if err == nil {
		return false
	}
	return !carrier.IsVoWiFiPolicyBlockedError(err)
}

func (a *hostAdapter) restoreRadioAfterVoWiFiStartupFailure(traceID, deviceID string) {
	db, m := a.currentBackend()
	if db == nil && m == nil {
		return
	}
	if restoreErr := db.SetOperatingMode(a.Context(), backend.ModeOnline); restoreErr != nil {
		logger.Warn("恢复射频失败", "trace_id", traceID, "device", deviceID, "err", restoreErr)
	}
}

func (a *hostAdapter) scheduleVoWiFiAPDUBusyRecover(deviceID, overrideEPDG string, generation uint64) {
	if a.manager == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	for _, delay := range []time.Duration{3 * time.Second, 5 * time.Second, 10 * time.Second} {
		delay := delay
		go func() {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-a.Context().Done():
				return
			case <-timer.C:
			}
			if a.manager.Active(deviceID) {
				return
			}
			if err := a.manager.Recover(a.Context(), vowifihost.LifecycleRecoverRequest{
				DeviceID:     deviceID,
				Reason:       "apdu_busy",
				OverrideEPDG: strings.TrimSpace(overrideEPDG),
				Generation:   generation,
			}); err != nil {
				logger.Debug("VoWiFi APDU busy 短退避恢复提交失败", "device", deviceID, "delay", delay.String(), "err", err)
			}
		}()
	}
}
