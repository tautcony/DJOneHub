package esim

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/euicc-go/driver"
	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/backend"
)

// esimPort 将现有 LPA manager 适配为应用后端端口。
// manager 为每个 LPA 会话创建全新的 APDU 通道，而底层 AT 传输仍由调用方
// （CommandBackend 或 ATBackend）持有。
type esimPort struct {
	manager *Manager
}

// NewATPort 创建基于纯 AT 命令传输的 eSIM 服务端口。command 由调用方注入：
// darwin 走 CommandBackend 的 USB AT 传输，Linux/Windows 走 modem.Manager 的
// 串口 AT 通道（ExecuteAT），因此两个平台路径共用同一实现。arbiter 必须是
// 设备级 APDU 仲裁器（与 modem manager 共享的同一实例）；它为纯 AT 路径启用
// SIM 切换 barrier 与 APDU idle 等待，而不是让这些防护成为空操作。
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
	return &esimPort{manager: manager}, nil
}

func (p *esimPort) Overview(ctx context.Context) (*EsimOverview, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.GetProfileOverview(ctx)
}

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
