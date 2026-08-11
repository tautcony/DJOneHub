package vowifi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// 本文件移植自 vohive-open internal/device（vowifi_modem_adapter.go /
// qmi_modem_adapter.go），将 DJOneHub 的 modem.Manager（AT 路径）与
// voWiFiBackend（QMI/MBIM 路径）适配为 vowifi-go 的 runtimehost.Modem
// + simauth.ATModem 接口。

type voWiFiBackend interface {
	Mode() string
	backend.SIMAuthProvider
	backend.OperatingModeController
	GetIMEI(context.Context) (string, error)
	GetIMSI(context.Context) (string, error)
	GetICCID(context.Context) (string, error)
	GetServingSystem(context.Context) (*backend.ServingSystem, error)
	IsSimInserted(context.Context) (bool, error)
	GetNativeMCCMNC(context.Context) (string, string, error)
}

// newVoWiFiModemInterface 按后端模式选择 AT（modem.Manager）或 QMI（backend）适配器。
func newVoWiFiModemInterface(db voWiFiBackend, m *modem.Manager, deviceID string) (runtimehost.Modem, error) {
	if db == nil && m == nil {
		return nil, fmt.Errorf("设备 %s 的 Modem/Backend 均未初始化，无法启动 VoWiFi", deviceID)
	}
	if db != nil {
		mode := strings.ToLower(strings.TrimSpace(db.Mode()))
		if mode != "" && mode != backend.BackendAT {
			return newQMIModemAdapter(deviceID, db), nil
		}
	}
	if m != nil {
		return newModemAdapter(m), nil
	}
	if db != nil {
		return newQMIModemAdapter(deviceID, db), nil
	}
	return nil, fmt.Errorf("设备 %s 无可用 VoWiFi modem adapter", deviceID)
}

// modemAdapter 将 AT 路径的 modem.Manager 适配为 runtimehost.Modem。
type modemAdapter struct {
	m *modem.Manager
}

func newModemAdapter(m *modem.Manager) *modemAdapter {
	return &modemAdapter{m: m}
}

func (a *modemAdapter) DeviceID() string                { return a.m.DeviceID() }
func (a *modemAdapter) IsHealthy() bool                 { return a.m.IsHealthy() }
func (a *modemAdapter) IsSimInserted() bool             { return a.m.IsSimInserted() }
func (a *modemAdapter) QuerySIMInserted() (bool, error) { return a.m.QuerySIMInserted() }
func (a *modemAdapter) GetRegStatus() (int, string)     { return a.m.GetRegStatus() }
func (a *modemAdapter) ExecuteATSilent(cmd string, timeout time.Duration) (string, error) {
	return a.m.ExecuteATSilent(cmd, timeout)
}
func (a *modemAdapter) OpenLogicalChannel(aid string) (int, error) {
	ch, err := a.m.OpenSIMAuthLogicalChannel(aid)
	return ch, normalizeVoWiFiAPDUError(err)
}
func (a *modemAdapter) ResolveLogicalChannelAID(app string, fallbackAID string) (string, string, error) {
	return a.m.ResolveSIMAuthAID(app, fallbackAID)
}
func (a *modemAdapter) CloseLogicalChannel(channel int) error {
	return normalizeVoWiFiAPDUError(a.m.CloseSIMAuthLogicalChannel(channel))
}
func (a *modemAdapter) TransmitAPDU(channel int, hexAPDU string) (string, error) {
	resp, err := a.m.TransmitAPDU(channel, hexAPDU)
	return resp, normalizeVoWiFiAPDUError(err)
}
func (a *modemAdapter) GetISIMIdentity() (identity.Identity, error) {
	return identity.ReadISIMIdentity(a)
}
func (a *modemAdapter) GetNetworkMode() string {
	mode := a.m.GetFullStatus().NetworkMode
	if mode == "" {
		if v, err := a.m.QueryNetworkModeFallback(); err == nil {
			mode = v
		}
	}
	return mode
}
func (a *modemAdapter) Stop() { a.m.Stop() }

// qmiModemAdapter 将逻辑通道后端适配为 runtimehost.Modem + simauth.ATModem。
// 它覆盖 QMI UIM 和 MBIM UICC。
type qmiModemAdapter struct {
	deviceID string
	backend  voWiFiBackend
}

func newQMIModemAdapter(deviceID string, b voWiFiBackend) *qmiModemAdapter {
	return &qmiModemAdapter{
		deviceID: deviceID,
		backend:  b,
	}
}

func (a *qmiModemAdapter) DeviceID() string { return a.deviceID }

// ExecuteATSilent 只保留接口占位；该适配器直接走逻辑通道。
func (a *qmiModemAdapter) ExecuteATSilent(cmd string, timeout time.Duration) (string, error) {
	return "", fmt.Errorf("QMI 模式不支持 AT 指令: %s", cmd)
}

func (a *qmiModemAdapter) OpenLogicalChannel(aid string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.backend.OpenLogicalChannel(ctx, aid)
	return ch, normalizeVoWiFiAPDUError(err)
}

func (a *qmiModemAdapter) ResolveLogicalChannelAID(app string, fallbackAID string) (string, string, error) {
	resolver, ok := a.backend.(backend.SIMAuthAIDResolver)
	if !ok {
		return fallbackAID, "fallback_backend_no_resolver", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return resolver.ResolveSIMAuthAID(ctx, app, fallbackAID)
}

func (a *qmiModemAdapter) CloseLogicalChannel(channel int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return normalizeVoWiFiAPDUError(a.backend.CloseLogicalChannel(ctx, channel))
}

func (a *qmiModemAdapter) TransmitAPDU(channel int, hexAPDU string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := a.backend.TransmitAPDU(ctx, channel, hexAPDU)
	return resp, normalizeVoWiFiAPDUError(err)
}

func (a *qmiModemAdapter) GetISIMIdentity() (identity.Identity, error) {
	return identity.ReadISIMIdentity(a)
}

func (a *qmiModemAdapter) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	present, err := a.backend.IsSimInserted(ctx)
	if err != nil {
		return false
	}
	return present
}

func (a *qmiModemAdapter) IsSimInserted() bool {
	return a.IsHealthy()
}

func (a *qmiModemAdapter) QuerySIMInserted() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return a.backend.IsSimInserted(ctx)
}

func (a *qmiModemAdapter) GetRegStatus() (int, string) {
	// VoWiFi 工作在飞行模式下，注册态仅用于日志，不影响启动判定
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ss, err := a.backend.GetServingSystem(ctx)
	if err != nil || ss == nil {
		return 0, "unknown"
	}
	return ss.RegStatus, ss.RegStatusText
}

func (a *qmiModemAdapter) GetNetworkMode() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ss, err := a.backend.GetServingSystem(ctx)
	if err != nil || ss == nil {
		return ""
	}
	return ss.NetworkMode
}

func (a *qmiModemAdapter) Stop() {
	logger.Info("QMI modem adapter Stop() 被调用（不关闭 Backend，由 Runtime 统一管理）",
		"device", a.deviceID)
}

func normalizeVoWiFiAPDUError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apduarbiter.ErrAPDUBusy) {
		return fmt.Errorf("%w: %v", runtimehost.ErrAPDUBusy, err)
	}
	return err
}
