package app

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/extras"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/notify"
)

// notificationChannelsNamespace 是远程通知渠道配置在 SQLite 里的命名空间，
// 与 notification_preferences（macOS 原生通知偏好）同构但互不影响。
const notificationChannelsNamespace = "notification_channels"

// multiSink 把一条通知事件同时投给多个 Sink。macOS 原生桥（Swift）与远程渠道
// 管理器在这里并列：用户勾选的每个渠道都会收到同一条事件。
//
// 单个 Sink 的实现自己负责不阻塞——native.Bridge 内部有投递队列，
// notify.Manager 的 Broadcast 为每个渠道另起 goroutine。
type multiSink []notification.Sink

var _ notification.Sink = (multiSink)(nil)

func (s multiSink) UpdateDeviceStatus(snapshot domain.Snapshot) {
	for _, sink := range s {
		sink.UpdateDeviceStatus(snapshot)
	}
}

func (s multiSink) UpdateNetwork(event notification.NetworkUpdateEvent) {
	for _, sink := range s {
		sink.UpdateNetwork(event)
	}
}

func (s multiSink) ShowCall(event notification.CallEvent) {
	for _, sink := range s {
		sink.ShowCall(event)
	}
}

func (s multiSink) UpdateCall(event notification.CallEvent) {
	for _, sink := range s {
		sink.UpdateCall(event)
	}
}

func (s multiSink) HideCall(event notification.CallEvent) {
	for _, sink := range s {
		sink.HideCall(event)
	}
}

func (s multiSink) ShowMissedCall(event notification.CallEvent) {
	for _, sink := range s {
		sink.ShowMissedCall(event)
	}
}

func (s multiSink) ShowSMS(event notification.SMSMessageEvent) {
	for _, sink := range s {
		sink.ShowSMS(event)
	}
}

func (s multiSink) ShowOffline(event notification.DeviceOfflineEvent) {
	for _, sink := range s {
		sink.ShowOffline(event)
	}
}

// notifyDependencies 聚合远程通知命令层需要的应用服务。
type notifyDependencies struct {
	devices *device.Service
	sms     *sms.Service
	esim    *esim.Service
	extras  *extras.Service
	network *network.Service
	ops     *operation.Manager
}

// newNotifyPorts 把应用服务适配成 notify.Ports。这是 notify 包与 application
// 层之间唯一的绑定点：notify 不 import application，避免循环依赖。
func newNotifyPorts(deps notifyDependencies) notify.Ports {
	return notify.Ports{
		DeviceStatus: func(ctx context.Context) (notify.DeviceStatus, error) {
			status, err := deps.devices.Status(ctx)
			if err != nil {
				return notify.DeviceStatus{}, err
			}
			return notify.DeviceStatus{
				State: string(status.Snapshot.State),
				// ready 是唯一的完全可用状态；degraded 仍可用但有降级原因。
				Healthy:     status.Snapshot.State == domain.StateReady,
				IMEI:        status.Identity.IMEI,
				ICCID:       firstNonEmpty(status.SIM.ICCID, status.Identity.ICCID),
				IMSI:        firstNonEmpty(status.SIM.IMSI, status.Identity.IMSI),
				Firmware:    status.Identity.Firmware,
				LocalPhone:  status.Identity.MSISDN,
				Operator:    status.Radio.Operator,
				NetworkMode: status.Radio.NetworkMode,
				Registered:  status.Radio.Registered,
				SIMInserted: status.SIM.Inserted,
				SignalDBM:   status.Radio.SignalDBM,
				SignalRSRP:  status.Radio.SignalRSRP,
				SignalRSRQ:  status.Radio.SignalRSRQ,
			}, nil
		},

		DeviceLabel: deviceLabel(deps.devices),

		SendSMS: deps.sms.Send,

		ListSMS: func(ctx context.Context, limit int) ([]notify.SMSRecord, error) {
			messages, err := deps.sms.List(ctx)
			if err != nil {
				return nil, err
			}
			records := make([]notify.SMSRecord, 0, len(messages))
			for _, message := range messages {
				records = append(records, smsRecord(message))
			}
			notify.SortSMSByTimeDesc(records)
			if limit > 0 && len(records) > limit {
				records = records[:limit]
			}
			return records, nil
		},

		ListESIM: func(ctx context.Context) ([]notify.ESIMProfile, error) {
			overview, err := deps.esim.Overview(ctx)
			if err != nil {
				return nil, err
			}
			// Overview 返回 map[string]any；探测失败时 profiles 是空切片。
			profiles, _ := overview["profiles"].([]backend.Profile)
			out := make([]notify.ESIMProfile, 0, len(profiles))
			for _, profile := range profiles {
				out = append(out, notify.ESIMProfile{
					ICCID:    profile.ICCID,
					Name:     firstNonEmpty(profile.Label, profile.ServiceProviderName),
					Provider: profile.ServiceProviderName,
					Active:   strings.EqualFold(profile.State, "enabled"),
				})
			}
			return out, nil
		},

		EnableESIM: deps.esim.Enable,

		Calls: func(ctx context.Context) (notify.CallSummary, error) {
			status := deps.extras.Calls(ctx)
			if status.Active == nil {
				return notify.CallSummary{}, nil
			}
			return notify.CallSummary{
				Active:    true,
				Direction: status.Active.Direction,
				State:     status.Active.State,
				Number:    status.Active.Number,
				StartedAt: status.Active.StartedAt,
			}, nil
		},

		Dial:   deps.extras.Dial,
		Reject: deps.extras.Reject,

		NetworkMode: func(ctx context.Context) (string, error) {
			status, err := deps.network.Status(ctx)
			if err != nil {
				return "", err
			}
			return firstNonEmpty(status.NetworkMode, status.Mode), nil
		},

		SetNetworkMode: deps.network.SetMode,

		AwaitOperation: awaitOperation(deps.ops),
	}
}

// deviceLabel 生成通知抬头用的设备名。它必须廉价且不阻塞——每条通知都会调用，
// 所以只读运行时快照，不下发 AT 命令。
func deviceLabel(devices *device.Service) func() string {
	return func() string {
		candidate, err := devices.RuntimeCandidate()
		if err != nil {
			return ""
		}
		return firstNonEmpty(candidate.Identity.Product, candidate.Identity.Manufacturer, candidate.StableID())
	}
}

// awaitOperation 轮询 operation 状态直到它进入终态或 ctx 超时。
// 轮询而非订阅事件总线：命令层的等待是一次性的短生命周期，订阅/退订的开销与
// 漏事件风险都不划算。
func awaitOperation(ops *operation.Manager) func(context.Context, string) (bool, string) {
	return func(ctx context.Context, operationID string) (bool, string) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, ok := ops.Get(operationID)
			if !ok {
				return false, "操作已丢失"
			}
			switch status.State {
			case operation.Succeeded:
				return true, status.Message
			case operation.Failed:
				if status.Error != nil {
					return false, status.Error.Message
				}
				return false, firstNonEmpty(status.Message, "操作失败")
			case operation.Cancelled:
				return false, "操作已取消"
			}
			select {
			case <-ctx.Done():
				return false, "等待超时"
			case <-ticker.C:
			}
		}
	}
}

func smsRecord(message backend.SMSMessage) notify.SMSRecord {
	// Recipient 非空即为发出的短信；对端号码随方向切换。
	outbound := strings.TrimSpace(message.Recipient) != ""
	peer := message.Sender
	if outbound {
		peer = message.Recipient
	}
	at := message.RecordedAt
	if at.IsZero() {
		at = message.ReceivedAt
	}
	return notify.SMSRecord{Outbound: outbound, Peer: peer, Body: message.Body, At: at}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
