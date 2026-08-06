package httpapi

import (
	"strings"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

// 公开事件流 (WebSocket 事件 + REST status/snapshot) 的字段级净化策略。
// 规则见 openspec 变更 cleanup-architectural-debt D7: 已知事件族用类型化投影,
// 原始 map 用字段白名单; 不在白名单上的字段一律不通过。错误/原因文本无条件
// 替换为回退文案, 不再使用 CJK 内容启发式 (docs/code-review-report.md 3.6 L4)。
// 设备身份 (IMEI/ICCID/IMSI/EID) 在 status/snapshot 中保持公开 — web Overview
// 卡片客户端侧掩码渲染, loopback 边界已保护非本地读者。
// REST 数据端点 (SMS 列表、通话历史) 不在本净化器范围内。

// networkUpdatedAllowlist 是 network.updated 原始 map 允许通过的字段:
// 注册状态、网络模式、频段与信号指标。
var networkUpdatedAllowlist = map[string]bool{
	"registered":   true,
	"network_mode": true,
	"radio_band":   true,
	"signal_dbm":   true,
	"signal_rsrp":  true,
	"signal_rsrq":  true,
	"signal_snr":   true,
}

// fallbackText 无条件以回退文案替换原始文本: 错误/原因字段不在公开白名单上。
func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return fallback
}

// sanitizeEvent 对 WS 事件流中的每个事件做家族级净化, 返回公开投影。
// 未识别家族 (含 backend.*) 不携带任何嵌套数据。
func sanitizeEvent(value runtime.Event) runtime.Event {
	switch value.Type {
	case "device.status.changed":
		if data, ok := value.Data.(domain.Snapshot); ok {
			value.Data = sanitizeSnapshot(data)
		}
	case "snapshot":
		if data, ok := value.Data.(device.Status); ok {
			value.Data = sanitizeDeviceStatus(data)
		}
	case "network.updated":
		if data, ok := value.Data.(map[string]any); ok {
			value.Data = sanitizeNetworkMap(data)
		}
	case notification.EventSMSReceived:
		if data, ok := value.Data.(notification.SMSMessageEvent); ok {
			value.Data = sanitizeSMSReceived(data)
		}
	case notification.EventCallIncoming, notification.EventCallUpdated, notification.EventCallEnded:
		if data, ok := value.Data.(notification.CallEvent); ok {
			value.Data = sanitizeCallEvent(data)
		}
	case "operation.progress", "operation.completed", "operation.changed":
		if data, ok := value.Data.(operation.Status); ok {
			value.Data = sanitizeOperationStatus(data)
		}
	case "operation.log":
		if data, ok := value.Data.(operation.Log); ok {
			data.Message = ""
			value.Data = data
		}
	default:
		// backend.* 与未知事件族: 不传递任何嵌套数据字段。
		value.Data = nil
	}
	return value
}

// sanitizeDeviceStatus 投影 REST device.Status: 嵌套 snapshot 走同一净化,
// identity/radio/sim 保持公开 (身份字段由 web Overview 客户端侧掩码渲染)。
func sanitizeDeviceStatus(value device.Status) device.Status {
	value.Snapshot = sanitizeSnapshot(value.Snapshot)
	return value
}

// sanitizeSnapshot 投影 domain.Snapshot: 保留 state/identity/backend/generation/
// capabilities 名称/错误外字段, 错误与原因文本替换为回退文案。
func sanitizeSnapshot(value domain.Snapshot) domain.Snapshot {
	value.BackendReason = fallbackText(value.BackendReason, "backend selection failed")
	value.LastError = fallbackText(value.LastError, "device error")
	if value.Capabilities != nil {
		capabilities := make(domain.CapabilitySet, len(value.Capabilities))
		for capability, reason := range value.Capabilities {
			capabilities[capability] = fallbackText(reason, "capability is unavailable")
		}
		value.Capabilities = capabilities
	}
	return value
}

// sanitizeSMSReceived 投影 sms.received: 只保留索引与时间戳, 正文/发送方/
// 接收方/ICCID 不出现在公开事件流 (web UI 通过 REST SMS 列表展示全文)。
func sanitizeSMSReceived(value notification.SMSMessageEvent) notification.SMSMessageEvent {
	value.Sender = ""
	value.Recipient = ""
	value.Body = ""
	value.ICCID = ""
	return value
}

// sanitizeCallEvent 投影 call.*: 保留会话元数据, 电话号码与原始 modem 文本不公开。
func sanitizeCallEvent(value notification.CallEvent) notification.CallEvent {
	value.Number = ""
	return value
}

// sanitizeOperationStatus 投影 operation.*: 保留结构化状态, 自由文本消息与
// 错误细节替换为稳定文案; 错误保留 code 作为机器可读契约。
func sanitizeOperationStatus(value operation.Status) operation.Status {
	value.Message = ""
	if value.Error != nil {
		value.Error = derrors.New(value.Error.Code, derrors.PublicMessage(value.Error.Code), value.Error.Retryable, nil)
	}
	return value
}

// sanitizeNetworkMap 对 network.updated 的原始 map 应用字段白名单:
// 不在白名单上的字段 (subscriber 身份、原始 backend 负载等) 一律不通过。
func sanitizeNetworkMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		if networkUpdatedAllowlist[key] {
			out[key] = item
		}
	}
	return out
}
