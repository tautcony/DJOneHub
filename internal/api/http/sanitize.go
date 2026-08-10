package httpapi

import (
	"strings"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

// 公开事件流 (WebSocket 事件 + REST status/snapshot) 的字段级净化策略。
// 已知敏感事件使用类型化投影。原始 map 使用明确的字段黑名单；未列入黑名单的
// 字段保留。错误/原因文本无条件替换为回退文案，不再使用 CJK 内容启发式。
// 设备身份 (IMEI/ICCID/IMSI/EID) 在 status/snapshot 中保持公开 — web Overview
// 卡片客户端侧掩码渲染, loopback 边界已保护非本地读者。
// REST 数据端点 (SMS 列表、通话历史) 不在本净化器范围内。

// publicEventKeyBlacklist contains field names that must not cross the public
// event boundary when an event uses a raw map. Matching is case-insensitive.
var publicEventKeyBlacklist = map[string]struct{}{
	"body":      {}, // SMSMessageEvent.Body contains the complete SMS text; raw SMS maps use the same key.
	"sender":    {}, // SMSMessageEvent.Sender contains the sender phone number; raw SMS maps use the same key.
	"iccid":     {}, // application/esim publishes the affected card ICCID in esim.updated maps.
	"data":      {}, // backend/mbim publishes the raw MBIM InfoBuffer in mbim.indication maps.
	"error":     {}, // backend/qmi publishes manager.Event.Error text in qmi.* maps.
	"cause":     {}, // Device-control operation errors attach raw transport or tool errors as cause.
	"path":      {}, // Device-control validation errors attach absolute loader, output, or EDL paths.
	"directory": {}, // EDL discovery errors attach the absolute configured EDL directory.
	"command":   {}, // ADB validation errors attach the configured executable, which can be an absolute path.
	"serial":    {}, // ADB selection errors attach the selected hardware serial.
}

// fallbackText 无条件以回退文案替换可能包含传输细节的错误或原因文本。
func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return fallback
}

// sanitizeEvent 对 WS 事件流中的每个事件做家族级净化, 返回公开投影。
// 未识别事件使用原始数据的敏感字段黑名单净化。
func sanitizeEvent(value runtime.Event) runtime.Event {
	switch value.Type {
	case "device.status.changed":
		if data, ok := value.Data.(domain.Snapshot); ok {
			value.Data = sanitizeSnapshot(data)
		} else {
			value.Data = sanitizePublicValue(value.Data)
		}
	case "snapshot":
		if data, ok := value.Data.(device.Status); ok {
			value.Data = sanitizeDeviceStatus(data)
		} else {
			value.Data = sanitizePublicValue(value.Data)
		}
	case "network.updated":
		if data, ok := value.Data.(map[string]any); ok {
			value.Data = sanitizeNetworkMap(data)
		}
	case notification.EventSMSReceived:
		if data, ok := value.Data.(notification.SMSMessageEvent); ok {
			value.Data = sanitizeSMSReceived(data)
		} else {
			value.Data = sanitizePublicValue(value.Data)
		}
	case notification.EventCallIncoming, notification.EventCallUpdated, notification.EventCallEnded:
		if data, ok := value.Data.(notification.CallEvent); ok {
			value.Data = sanitizeCallEvent(data)
		} else {
			value.Data = sanitizePublicValue(value.Data)
		}
	case "operation.progress", "operation.completed", "operation.changed":
		if data, ok := value.Data.(operation.Status); ok {
			value.Data = sanitizeOperationStatus(data)
		} else {
			value.Data = sanitizePublicValue(value.Data)
		}
	case "operation.log":
		// xterm consumes the exact process stream. Preserve ANSI sequences,
		// carriage returns, newlines, and chunk boundaries.
		if _, ok := value.Data.(operation.Log); !ok {
			value.Data = sanitizePublicValue(value.Data)
		}
	default:
		value.Data = sanitizePublicValue(value.Data)
	}
	return value
}

// sanitizeDeviceStatus 投影 REST device.Status: 嵌套 snapshot 走同一净化,
// identity/radio/sim 保持公开 (身份字段由 web Overview 客户端侧掩码渲染)。
func sanitizeDeviceStatus(value device.Status) device.Status {
	value.Snapshot = sanitizeSnapshot(value.Snapshot)
	return value
}

// sanitizeSnapshot 投影 domain.Snapshot 并替换错误和原因文本。
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

// sanitizeOperationStatus preserves progress text and applies the raw-map
// blacklist to structured error details.
func sanitizeOperationStatus(value operation.Status) operation.Status {
	if value.Error != nil {
		details := sanitizePublicMap(value.Error.Details)
		if len(details) == 0 {
			details = nil
		}
		value.Error = derrors.New(value.Error.Code, derrors.PublicMessage(value.Error.Code), value.Error.Retryable, details)
	}
	return value
}

// sanitizeNetworkMap 对 network.updated 的原始 map 应用字段黑名单。
func sanitizeNetworkMap(value map[string]any) map[string]any {
	return sanitizePublicMap(value)
}

func sanitizePublicValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		return sanitizePublicMap(item)
	case []any:
		out := make([]any, len(item))
		for index, nested := range item {
			out[index] = sanitizePublicValue(nested)
		}
		return out
	default:
		return value
	}
}

func sanitizePublicMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		if _, blocked := publicEventKeyBlacklist[strings.ToLower(strings.TrimSpace(key))]; blocked {
			continue
		}
		out[key] = sanitizePublicValue(item)
	}
	return out
}
