package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

// TestSanitizeSnapshotReplacesErrorTextWithoutHeuristic 错误/原因文本无条件
// 替换为回退文案 (英文错误不再因缺少 CJK 而原样通过)。
func TestSanitizeSnapshotReplacesErrorTextWithoutHeuristic(t *testing.T) {
	value := domain.Snapshot{
		State:         domain.StateReady,
		BackendReason: "connection refused: dial tcp 10.0.0.1:8080",
		LastError:     "AT+CGDCONT? returned ERROR",
		Capabilities:  domain.CapabilitySet{"sms": "sms worker crashed: panic at 0x1234"},
		Generation:    7,
	}
	out := sanitizeSnapshot(value)
	if out.BackendReason != "backend selection failed" {
		t.Fatalf("BackendReason = %q, want fallback", out.BackendReason)
	}
	if out.LastError != "device error" {
		t.Fatalf("LastError = %q, want fallback", out.LastError)
	}
	if got := out.Capabilities["sms"]; got != "capability is unavailable" {
		t.Fatalf("capability reason = %q, want fallback", got)
	}
	if out.State != domain.StateReady || out.Generation != 7 {
		t.Fatalf("allowlisted fields were altered: %+v", out)
	}
}

// TestSanitizeSnapshotKeepsIdentity 身份字段在 status/snapshot 中保持公开:
// web Overview 卡片客户端侧掩码渲染依赖它们。
func TestSanitizeSnapshotKeepsIdentity(t *testing.T) {
	identity := domain.Identity{IMEI: "990000860099326", VendorID: "2c7c", Product: "DJI/Quectel AT modem"}
	status := device.Status{
		Snapshot: domain.Snapshot{State: domain.StateReady, Identity: identity, LastError: "modem error"},
		Identity: backend.Identity{IMEI: "990000860099326", ICCID: "89860012345678901234", IMSI: "460009300011111"},
		Radio:    backend.RadioState{Registered: true, NetworkMode: "LTE", SignalDBM: -87},
		SIM:      backend.SIMState{Inserted: true, ICCID: "89860012345678901234", IMSI: "460009300011111"},
	}
	out := sanitizeDeviceStatus(status)
	if out.Snapshot.Identity.IMEI != identity.IMEI || out.Snapshot.Identity.VendorID != identity.VendorID {
		t.Fatalf("snapshot identity was redacted: %+v", out.Snapshot.Identity)
	}
	if out.Identity.IMEI != status.Identity.IMEI || out.Identity.ICCID != status.Identity.ICCID || out.Identity.IMSI != status.Identity.IMSI {
		t.Fatalf("status identity was redacted: %+v", out.Identity)
	}
	if !out.Radio.Registered || out.Radio.NetworkMode != "LTE" || !out.SIM.Inserted {
		t.Fatalf("radio/sim fields were redacted: %+v", out)
	}
	if out.Snapshot.LastError != "device error" {
		t.Fatalf("LastError = %q, want fallback", out.Snapshot.LastError)
	}
}

// TestSanitizeEventMatrix 按事件族验证公开投影: 白名单字段通过, 其余字段
// (含未知类型) 一律不通过。
func TestSanitizeEventMatrix(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		event runtime.Event
		want any // 期望的净化的 Data
	}{
		{
			name: "sms.received redacts content",
			event: runtime.Event{Type: "sms.received", Data: notification.SMSMessageEvent{
				Index: 3, Sender: "10086", Recipient: "13800000000", Body: "验证码 123456",
				ReceivedAt: now, RecordedAt: now, ICCID: "89860012345678901234",
			}},
			want: notification.SMSMessageEvent{Index: 3, ReceivedAt: now, RecordedAt: now},
		},
		{
			name: "call.incoming redacts number",
			event: runtime.Event{Type: "call.incoming", Data: notification.CallEvent{
				ID: "call-1", Direction: "incoming", State: "incoming", Number: "01012345678",
				StartedAt: now, Missed: true,
			}},
			want: notification.CallEvent{ID: "call-1", Direction: "incoming", State: "incoming", StartedAt: now, Missed: true},
		},
		{
			name: "operation.progress redacts message and error details",
			event: runtime.Event{Type: "operation.progress", Data: operation.Status{
				ID: "op-1", Type: "sms.send", State: operation.Failed, Progress: 50,
				Message: "下发失败: +CMGS ERROR", StartedAt: now, FinishedAt: now,
				Error: derrors.New(derrors.Internal, "backend crashed: 0xDEAD", true, map[string]any{"path": "/dev/ttyUSB2"}),
			}},
			want: operation.Status{
				ID: "op-1", Type: "sms.send", State: operation.Failed, Progress: 50,
				StartedAt: now, FinishedAt: now,
				Error: derrors.New(derrors.Internal, derrors.PublicMessage(derrors.Internal), true, nil),
			},
		},
		{
			name: "network.updated allowlist only",
			event: runtime.Event{Type: "network.updated", Data: map[string]any{
				"registered": true, "network_mode": "LTE", "radio_band": "BAND 8",
				"signal_dbm": -87, "subscriber_imsi": "460009300011111", "raw": "AT+QNWINFO payload",
			}},
			want: map[string]any{"registered": true, "network_mode": "LTE", "radio_band": "BAND 8", "signal_dbm": -87},
		},
		{
			name:  "backend event carries no data",
			event: runtime.Event{Type: "backend.disconnected", Data: map[string]any{"error": "modem unplugged", "serial": "0123456789ABCDEF"}},
			want:  nil,
		},
		{
			name:  "unknown event family carries no data",
			event: runtime.Event{Type: "esim.updated", Data: map[string]any{"profiles": []string{"A"}}},
			want:  nil,
		},
		{
			name: "device.status.changed keeps snapshot with identity",
			event: runtime.Event{Type: "device.status.changed", Data: domain.Snapshot{
				State: domain.StateReady, Identity: domain.Identity{IMEI: "990000860099326"},
				BackendReason: "no modem", LastError: "device error text",
			}},
			want: domain.Snapshot{State: domain.StateReady, Identity: domain.Identity{IMEI: "990000860099326"},
				BackendReason: "backend selection failed", LastError: "device error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := sanitizeEvent(tt.event)
			if out.Type != tt.event.Type || out.ID != tt.event.ID {
				t.Fatalf("event envelope altered: %+v", out)
			}
			gotJSON, _ := json.Marshal(out.Data)
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("sanitized data = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// TestSanitizeEventRawMapNoUnknownPassthrough 原始 map 事件绝不透传未知字段。
func TestSanitizeEventRawMapNoUnknownPassthrough(t *testing.T) {
	out := sanitizeEvent(runtime.Event{Type: "network.updated", Data: map[string]any{
		"registered":   false,
		"surprise_key": "leaked",
	}})
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map", out.Data)
	}
	if _, exists := data["surprise_key"]; exists {
		t.Fatal("unknown field passed through the allowlist")
	}
	if registered, ok := data["registered"]; !ok || registered != false {
		t.Fatalf("allowlisted field missing or wrong: %+v", data)
	}
}
