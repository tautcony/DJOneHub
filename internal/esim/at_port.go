package esim

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/euicc-go/driver"
	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/simaid"
)

// esimPort 将现有 LPA manager 适配为应用后端端口。
// manager 为每个 LPA 会话创建全新的 APDU 通道，而底层 AT 传输仍由调用方
// （ATBackend）持有。
type esimPort struct {
	manager *Manager
}

// NewATPort 创建基于纯 AT 命令传输的 eSIM 服务端口。command 由 AT manager 注入，
// 因此 macOS USB、Linux 串口和 Windows 串口共用同一实现。arbiter 必须是
// 设备级 APDU 仲裁器（与 modem manager 共享的同一实例）；它为纯 AT 路径启用
// SIM 切换 barrier 与 APDU idle 等待，而不是让这些防护成为空操作。eSIM
// 操作通过 AT+CSIM 读取基本通道，或通过 AT+CCHO/AT+CGLA/AT+CCHC 打开、
// 透传、关闭逻辑通道。
func NewATPort(candidateID string, arbiter *apduarbiter.Arbiter, command func(string, time.Duration) (string, error), imei func(context.Context) (string, error), iccid func(context.Context) (string, error)) (backend.ESIMPort, error) {
	manager, err := NewManager(ManagerOptions{
		DeviceID:             candidateID,
		Transport:            "custom",
		IMEIProvider:         imei,
		ICCIDProvider:        iccid,
		APDUArbiter:          arbiter,
		SwitchUseRefreshTrue: true,
		SmartCardChannelFactory: func() (driver.SmartCardChannel, error) {
			return NewSmartCardChannel(newATCommandTransport(command)), nil
		},
	})
	if err != nil {
		return nil, err
	}
	manager.cardProbe = func(ctx context.Context) (bool, error) {
		aids, err := simaid.ReadDirectoryAIDs(func(apdu []byte) ([]byte, error) {
			return transmitBasicCSIM(ctx, command, apdu)
		})
		if err != nil {
			return false, fmt.Errorf("读取 EF_DIR 失败，暂不能判断卡类型: %w", err)
		}
		for _, aid := range aids {
			for _, candidate := range AIDs {
				if strings.EqualFold(hex.EncodeToString(aid), hex.EncodeToString(candidate)) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	return &esimPort{manager: manager}, nil
}

func transmitBasicCSIM(ctx context.Context, command func(string, time.Duration) (string, error), apdu []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	apduHex := strings.ToUpper(hex.EncodeToString(apdu))
	resp, err := command(fmt.Sprintf("AT+CSIM=%d,\"%s\"", len(apduHex), apduHex), 5*time.Second)
	if err != nil {
		return nil, err
	}
	value, ok := parseBasicCSIMResponse(resp)
	if !ok {
		return nil, fmt.Errorf("AT+CSIM 响应无效")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 2 && decoded[0] == 0x61 {
		getResponse := []byte{0x00, 0xC0, 0x00, 0x00, decoded[1]}
		return transmitBasicCSIM(ctx, command, getResponse)
	}
	return decoded, nil
}

func parseBasicCSIMResponse(resp string) (string, bool) {
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CSIM:") {
			continue
		}
		start, end := strings.Index(line, "\""), strings.LastIndex(line, "\"")
		if start >= 0 && end > start {
			return strings.TrimSpace(line[start+1 : end]), true
		}
	}
	return "", false
}

// Overview reads a lightweight eSIM profile snapshot. On an AT transport it
// may send AT+CSIM for EF_DIR, then AT+CCHO, AT+CGLA, and AT+CCHC for AID
// discovery, EID reads, and profile APDUs.
func (p *esimPort) Overview(ctx context.Context) (*EsimOverview, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.GetProfileOverview(ctx)
}

// EID returns the first EID from Overview. It reuses the Overview cache when
// warm; a cold AT path performs the AT+CSIM and AT+CCHO/AT+CGLA/AT+CCHC scan
// described by Overview.
func (p *esimPort) EID(ctx context.Context) (string, error) {
	overview, err := p.Overview(ctx)
	if err != nil {
		return "", err
	}
	if overview == nil || overview.ChipInfo == nil || len(overview.ChipInfo.EIDs) == 0 {
		return "", fmt.Errorf("eUICC EID is unavailable")
	}
	return overview.ChipInfo.EIDs[0].EID, nil
}

// Profiles returns profile records from Overview and may issue AT+QCCID through
// the live ICCID provider to correct the active state.
func (p *esimPort) Profiles(ctx context.Context) ([]backend.Profile, error) {
	overview, err := p.Overview(ctx)
	if err != nil {
		return nil, err
	}
	profiles := make([]backend.Profile, 0)
	for _, group := range overview.Profiles {
		for _, item := range group.Profiles {
			state := "unknown"
			if item.StateKnown {
				switch item.State {
				case 0:
					state = "disabled"
				case 1:
					state = "enabled"
				default:
					state = "unknown"
				}
			}
			var stateCode *int
			if item.StateKnown {
				code := item.State
				stateCode = &code
			}
			profiles = append(profiles, backend.Profile{
				ICCID:               item.ICCID,
				State:               state,
				StateCode:           stateCode,
				StateKnown:          item.StateKnown,
				Label:               firstNonEmpty(item.Name, item.ServiceProviderName),
				EID:                 group.EID,
				AID:                 group.AIDHex,
				ServiceProviderName: item.ServiceProviderName,
				ProfileClass:        item.ClassText,
			})
		}
	}
	if activeICCID, err := p.manager.CurrentICCID(ctx); err == nil {
		for index := range profiles {
			if strings.EqualFold(strings.TrimSpace(profiles[index].ICCID), strings.TrimSpace(activeICCID)) {
				profiles[index].State = "enabled"
				profiles[index].StateCode = intPointer(1)
				profiles[index].StateKnown = true
			}
		}
	}
	return profiles, nil
}

func (p *esimPort) ESIMStorage(ctx context.Context) (backend.ESIMStorageInfo, error) {
	if p == nil || p.manager == nil {
		return backend.ESIMStorageInfo{}, fmt.Errorf("eSIM manager is unavailable")
	}
	info, err := p.manager.GetEUICCChipInfo(ctx, false)
	if err != nil {
		return backend.ESIMStorageInfo{}, err
	}
	if info == nil || len(info.EIDs) == 0 {
		return backend.ESIMStorageInfo{}, fmt.Errorf("eUICC storage information is unavailable")
	}
	return backend.ESIMStorageInfo{
		FreeNvramBytes: info.EIDs[0].FreeNvramBytes,
		FreeNvram:      info.EIDs[0].FreeNvram,
	}, nil
}

func (p *esimPort) ESIMDeviceInfo(ctx context.Context) (backend.ESIMDeviceInfo, error) {
	if p == nil || p.manager == nil {
		return backend.ESIMDeviceInfo{}, fmt.Errorf("eSIM manager is unavailable")
	}
	info, err := p.manager.GetEUICCChipInfo(ctx, false)
	if err != nil {
		return backend.ESIMDeviceInfo{}, err
	}
	if info == nil {
		return backend.ESIMDeviceInfo{}, fmt.Errorf("eUICC device information is unavailable")
	}
	return backend.ESIMDeviceInfo{
		SKU:          info.SkuName,
		SerialNumber: info.SerialNumber,
		Firmware:     info.Firmware,
	}, nil
}

// ESIMSnapshot performs one full eSIM snapshot. The AT path uses AT+CSIM for
// card probing, AT+CCHO/AT+CGLA/AT+CCHC for eUICC and profile APDUs, and the
// same logical-channel sequence for optional product information.
func (p *esimPort) ESIMSnapshot(ctx context.Context) (backend.ESIMSnapshot, error) {
	if p == nil || p.manager == nil {
		return backend.ESIMSnapshot{}, fmt.Errorf("eSIM manager is unavailable")
	}
	overview, err := p.manager.GetEsimOverview(ctx)
	if err != nil {
		return backend.ESIMSnapshot{}, err
	}
	if overview == nil || overview.ChipInfo == nil || len(overview.ChipInfo.EIDs) == 0 {
		return backend.ESIMSnapshot{}, fmt.Errorf("eUICC device information is unavailable")
	}
	profiles := make([]backend.Profile, 0)
	for _, group := range overview.Profiles {
		for _, item := range group.Profiles {
			state := "unknown"
			if item.StateKnown {
				switch item.State {
				case 0:
					state = "disabled"
				case 1:
					state = "enabled"
				}
			}
			profiles = append(profiles, backend.Profile{
				ICCID: item.ICCID, State: state, StateKnown: item.StateKnown,
				Label: firstNonEmpty(item.Name, item.ServiceProviderName), EID: group.EID,
				AID: group.AIDHex, ServiceProviderName: item.ServiceProviderName, ProfileClass: item.ClassText,
			})
		}
	}
	eid := overview.ChipInfo.EIDs[0].EID
	return backend.ESIMSnapshot{
		EID: eid, Profiles: profiles,
		DeviceInfo: backend.ESIMDeviceInfo{SKU: overview.ChipInfo.SkuName, SerialNumber: overview.ChipInfo.SerialNumber, Firmware: overview.ChipInfo.Firmware},
		Storage:    backend.ESIMStorageInfo{FreeNvramBytes: overview.ChipInfo.EIDs[0].FreeNvramBytes, FreeNvram: overview.ChipInfo.EIDs[0].FreeNvram},
	}, nil
}

func (p *esimPort) Download(ctx context.Context, activationCode, confirmationCode, matchingID string, opts *backend.ESIMDownloadOptions) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	var interact *DownloadInteraction
	if opts != nil && opts.ConfirmationCodeRequest != nil {
		interact = &DownloadInteraction{OnConfirmationCodeRequest: opts.ConfirmationCodeRequest}
	}
	var progressFn DownloadProgressFn
	if opts != nil && opts.Progress != nil {
		progressFn = func(event DownloadProgressEvent) {
			opts.Progress(event.Step, event.Pct, event.Msg)
		}
	}
	_, err := p.manager.DownloadProfile(ctx, "", activationCode, matchingID, confirmationCode, "", progressFn, interact)
	return err
}

func (p *esimPort) Enable(ctx context.Context, iccid string) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.SwitchProfile(ctx, strings.TrimSpace(iccid), "")
}

func (p *esimPort) Disable(ctx context.Context, iccid string) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.DisableProfile(ctx, strings.TrimSpace(iccid), "")
}

func (p *esimPort) ListNotifications(ctx context.Context) ([]backend.NotificationItem, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("eSIM manager is unavailable")
	}
	items, err := p.manager.ListNotificationsContext(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]backend.NotificationItem, 0, len(items))
	for _, item := range items {
		out = append(out, backend.NotificationItem{
			SequenceNumber: item.SequenceNumber,
			Event:          item.Event,
			ICCID:          item.ICCID,
			Address:        item.Address,
			CanRetry:       item.CanRetry,
		})
	}
	return out, nil
}

func (p *esimPort) ProcessNotification(ctx context.Context, sequenceNumber int64) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.RetryNotification(sequenceNumber, "")
}

func (p *esimPort) RemoveNotification(ctx context.Context, sequenceNumber int64) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.RemoveNotification(sequenceNumber, "")
}

func (p *esimPort) Rename(ctx context.Context, iccid, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.RenameProfile(strings.TrimSpace(iccid), strings.TrimSpace(label), "")
}

func (p *esimPort) Delete(ctx context.Context, iccid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	_, err := p.manager.DeleteProfile(strings.TrimSpace(iccid), "")
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func intPointer(value int) *int { return &value }
