package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
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
		t.Fatalf("non-sensitive fields were altered: %+v", out)
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

// TestSanitizeEventMatrix verifies typed redaction and raw-map blacklisting.
func TestSanitizeEventMatrix(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		event runtime.Event
		want  any // 期望的净化的 Data
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
			name: "operation.progress keeps bounded message and safe error details",
			event: runtime.Event{Type: "operation.progress", Data: operation.Status{
				ID: "op-1", Type: "sms.send", State: operation.Failed, Progress: 50,
				Message: "下发失败: +CMGS ERROR", StartedAt: now, FinishedAt: now,
				Error: derrors.New(derrors.Internal, "backend crashed: 0xDEAD", true, map[string]any{"path": "/dev/ttyUSB2", "phase": "read_nand"}),
			}},
			want: operation.Status{
				ID: "op-1", Type: "sms.send", State: operation.Failed, Progress: 50,
				Message:   "下发失败: +CMGS ERROR",
				StartedAt: now, FinishedAt: now,
				Error: derrors.New(derrors.Internal, derrors.PublicMessage(derrors.Internal), true, map[string]any{"phase": "read_nand"}),
			},
		},
		{
			name: "operation.log keeps the exact terminal stream",
			event: runtime.Event{Type: "operation.log", Data: operation.Log{
				OperationID: "op-1", Type: "device_control.nand_backup", Message: "\x1b[2Kread_nand: 50%\r\n",
			}},
			want: operation.Log{OperationID: "op-1", Type: "device_control.nand_backup", Message: "\x1b[2Kread_nand: 50%\r\n"},
		},
		{
			name: "network.updated keeps current non-sensitive fields",
			event: runtime.Event{Type: "network.updated", Data: map[string]any{
				"registered": true, "network_mode": "LTE", "radio_band": "BAND 8", "signal_dbm": -87,
			}},
			want: map[string]any{"registered": true, "network_mode": "LTE", "radio_band": "BAND 8", "signal_dbm": -87},
		},
		{
			name:  "backend event removes blacklisted fields",
			event: runtime.Event{Type: "backend.disconnected", Data: map[string]any{"error": "modem unplugged", "serial": "0123456789ABCDEF"}},
			want:  map[string]any{},
		},
		{
			name:  "unknown event family keeps non-sensitive data",
			event: runtime.Event{Type: "esim.updated", Data: map[string]any{"profiles": []string{"A"}}},
			want:  map[string]any{"profiles": []string{"A"}},
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
		{
			name: "EDL session event masks protocol identifiers and location",
			event: runtime.Event{Type: "device_control.edl_session_changed", Data: domain.EDLSessionSnapshot{
				SessionID: "session-1", PhysicalLocation: "usb/1-2", ActiveOperation: "device_control.adb_shell",
				Observation: domain.EDLObservation{State: domain.EDLStateSaharaIdentified, SerialNumber: "12345678", HardwareID: "0102030405060708", PKHash: "aabbccdd"},
			}},
			want: domain.EDLSessionSnapshot{SessionID: "session-1", ActiveOperation: "device_control.adb_shell",
				Observation: domain.EDLObservation{State: domain.EDLStateSaharaIdentified, SerialNumber: "****5678", HardwareID: "****0708", PKHash: "****ccdd"}},
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

// TestSanitizeEventRawMapUsesBlacklist keeps new fields and removes fields
// that occur with sensitive content in current event producers.
func TestSanitizeEventRawMapUsesBlacklist(t *testing.T) {
	out := sanitizeEvent(runtime.Event{Type: "esim.updated", Data: map[string]any{
		"operation":    "enable",
		"surprise_key": "retained",
		"number":       "+10000000000",
		"recipient":    "+10000000001",
		"iccid":        "89860012345678901234",
		"nested":       map[string]any{"state": "ready", "data": []byte{0x01, 0x02}},
	}})
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map", out.Data)
	}
	if data["surprise_key"] != "retained" {
		t.Fatalf("new non-sensitive field was removed: %+v", data)
	}
	if operationName, ok := data["operation"]; !ok || operationName != "enable" {
		t.Fatalf("existing field missing or wrong: %+v", data)
	}
	if data["number"] != "+10000000000" || data["recipient"] != "+10000000001" {
		t.Fatalf("fields without a sensitive raw-map producer were removed: %+v", data)
	}
	if _, exists := data["iccid"]; exists {
		t.Fatalf("blacklisted ICCID was retained: %+v", data)
	}
	nested, ok := data["nested"].(map[string]any)
	if !ok || nested["state"] != "ready" {
		t.Fatalf("nested non-sensitive data was removed: %+v", data)
	}
	if _, exists := nested["data"]; exists {
		t.Fatalf("nested raw MBIM data was retained: %+v", data)
	}
}
