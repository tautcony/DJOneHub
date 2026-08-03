package notification

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// fixtureEnvelope mirrors runtime.Event JSON tags; fixtures must keep decoding
// into the real envelope type when events flow over the EventBus.
type fixtureEnvelope struct {
	ID         uint64          `json:"id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

func readFixture(t *testing.T, path string) fixtureEnvelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var envelope fixtureEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", path, err)
	}
	return envelope
}

func requireField(t *testing.T, envelope fixtureEnvelope, event string, data json.RawMessage) {
	t.Helper()
	if envelope.ID == 0 {
		t.Errorf("fixture event id must be positive")
	}
	if envelope.Version != EventVersion {
		t.Errorf("version = %d, want %d", envelope.Version, EventVersion)
	}
	if envelope.Type != event {
		t.Errorf("type = %q, want %q", envelope.Type, event)
	}
	if envelope.OccurredAt.IsZero() {
		t.Errorf("occurred_at must be set")
	}
	if len(data) == 0 {
		t.Errorf("fixture data must not be empty")
	}
}

func decode[target any](t *testing.T, data json.RawMessage) target {
	t.Helper()
	var value target
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %T: %v", value, err)
	}
	return value
}

func TestContractEventFixturesDecode(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		file   string
		event  string
		assert func(t *testing.T, data json.RawMessage)
	}{
		{
			name:  "call.incoming",
			file:  "testdata/call_incoming.json",
			event: EventCallIncoming,
			assert: func(t *testing.T, data json.RawMessage) {
				call := decode[CallEvent](t, data)
				if call.ID != "1783069200000-1" || call.Direction != "incoming" || call.State != "incoming" || call.Number != "18900007376" {
					t.Errorf("call = %+v", call)
				}
				if !call.StartedAt.Equal(now) {
					t.Errorf("started_at = %v, want %v", call.StartedAt, now)
				}
				if call.Missed || call.EndedAt != nil {
					t.Errorf("incoming call must not be missed or ended: %+v", call)
				}
			},
		},
		{
			name:  "call.updated",
			file:  "testdata/call_updated.json",
			event: EventCallUpdated,
			assert: func(t *testing.T, data json.RawMessage) {
				call := decode[CallEvent](t, data)
				if call.ID != "1783069200000-1" || call.State != "active" {
					t.Errorf("call = %+v", call)
				}
			},
		},
		{
			name:  "call.ended",
			file:  "testdata/call_ended.json",
			event: EventCallEnded,
			assert: func(t *testing.T, data json.RawMessage) {
				call := decode[CallEvent](t, data)
				if call.EndedAt == nil || !call.EndedAt.Equal(now.Add(90*time.Second)) {
					t.Errorf("ended_at = %v", call.EndedAt)
				}
				if call.Missed {
					t.Errorf("answered call must not be missed")
				}
			},
		},
		{
			name:  "call.missed",
			file:  "testdata/call_missed.json",
			event: EventCallMissed,
			assert: func(t *testing.T, data json.RawMessage) {
				call := decode[CallEvent](t, data)
				if !call.Missed || call.EndedAt == nil {
					t.Errorf("call = %+v", call)
				}
			},
		},
		{
			name:  "sms.received",
			file:  "testdata/sms_received.json",
			event: EventSMSReceived,
			assert: func(t *testing.T, data json.RawMessage) {
				message := decode[SMSMessageEvent](t, data)
				if message.Index != 7 || message.Sender != "10086" || message.Code != "482913" {
					t.Errorf("message = %+v", message)
				}
				if message.Body != "您的验证码是 482913" {
					t.Errorf("body = %q", message.Body)
				}
				wantKey := "10086\x00\x00您的验证码是 482913\x002026-08-02T10:00:05Z"
				if key := message.DedupKey(); key != wantKey {
					t.Errorf("dedup key = %q, want %q", key, wantKey)
				}
			},
		},
		{
			name:  "gps.updated",
			file:  "testdata/gps_updated.json",
			event: EventGPSUpdated,
			assert: func(t *testing.T, data json.RawMessage) {
				status := decode[GPSUpdateEvent](t, data)
				if !status.Enabled || status.Fix == nil {
					t.Fatalf("status = %+v", status)
				}
				if status.Fix.Latitude != "31.2304" || status.Fix.Longitude != "121.4737" || status.Fix.HDOP != "1.1" || status.Fix.Satellites != "12" {
					t.Errorf("fix = %+v", status.Fix)
				}
			},
		},
		{
			name:  "gps.updated searching",
			file:  "testdata/gps_updated_searching.json",
			event: EventGPSUpdated,
			assert: func(t *testing.T, data json.RawMessage) {
				status := decode[GPSUpdateEvent](t, data)
				if !status.Enabled || status.Fix != nil {
					t.Errorf("searching status = %+v", status)
				}
			},
		},
		{
			name:  "device.status.changed",
			file:  "testdata/device_status_changed.json",
			event: EventDeviceStatusChanged,
			assert: func(t *testing.T, data json.RawMessage) {
				var raw map[string]any
				if err := json.Unmarshal(data, &raw); err != nil {
					t.Fatalf("decode snapshot: %v", err)
				}
				if raw["state"] != "ready" || raw["generation"] != float64(5) {
					t.Errorf("snapshot = %v", raw)
				}
			},
		},
		{
			name:  "device.offline",
			file:  "testdata/device_offline.json",
			event: EventDeviceOffline,
			assert: func(t *testing.T, data json.RawMessage) {
				offline := decode[DeviceOfflineEvent](t, data)
				if offline.State != "disconnected" || offline.Reason == "" {
					t.Errorf("offline = %+v", offline)
				}
			},
		},
		{
			name:  "network.updated",
			file:  "testdata/network_updated.json",
			event: EventNetworkUpdated,
			assert: func(t *testing.T, data json.RawMessage) {
				state := decode[NetworkUpdateEvent](t, data)
				if !state.Registered || state.NetworkMode != "LTE" || state.SignalDBM != -83 {
					t.Errorf("network = %+v", state)
				}
			},
		},
		{
			name:  "call.reject.started",
			file:  "testdata/call_reject_started.json",
			event: EventCallRejectStarted,
			assert: func(t *testing.T, data json.RawMessage) {
				result := decode[RejectResult](t, data)
				if result.CallID != "1783069200000-1" || result.Error != "" {
					t.Errorf("result = %+v", result)
				}
			},
		},
		{
			name:  "call.reject.succeeded",
			file:  "testdata/call_reject_succeeded.json",
			event: EventCallRejectSucceeded,
			assert: func(t *testing.T, data json.RawMessage) {
				result := decode[RejectResult](t, data)
				if result.CallID != "1783069200000-1" {
					t.Errorf("result = %+v", result)
				}
			},
		},
		{
			name:  "call.reject.failed",
			file:  "testdata/call_reject_failed.json",
			event: EventCallRejectFailed,
			assert: func(t *testing.T, data json.RawMessage) {
				result := decode[RejectResult](t, data)
				if result.Error == "" {
					t.Errorf("failed result must carry error: %+v", result)
				}
			},
		},
		{
			name:  "dashboard.opened",
			file:  "testdata/dashboard_opened.json",
			event: EventDashboardOpened,
			assert: func(t *testing.T, data json.RawMessage) {
				opened := decode[DashboardOpened](t, data)
				if opened.URL != "http://127.0.0.1:7575/" {
					t.Errorf("opened = %+v", opened)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := readFixture(t, test.file)
			requireField(t, envelope, test.event, envelope.Data)
			test.assert(t, envelope.Data)
		})
	}
}

func TestContractCommandFixturesDecode(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		command Command
	}{
		{name: "reject_call", file: "testdata/command_reject_call.json", command: Command{Name: CommandRejectCall, Params: map[string]string{"call_id": "1783069200000-1"}}},
		{name: "open_dashboard", file: "testdata/command_open_dashboard.json", command: Command{Name: CommandOpenDashboard}},
		{name: "toggle_gps_panel", file: "testdata/command_toggle_gps_panel.json", command: Command{Name: CommandToggleGPSPanel}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(test.file)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			command := decode[Command](t, raw)
			if command.Name != test.command.Name {
				t.Errorf("name = %q, want %q", command.Name, test.command.Name)
			}
			if test.command.Name == CommandRejectCall && command.CallID() != "1783069200000-1" {
				t.Errorf("call_id = %q", command.CallID())
			}
			wantReject := test.command.Name == CommandRejectCall
			if command.IsRejectCall() != wantReject {
				t.Errorf("IsRejectCall() = %v, want %v for %+v", command.IsRejectCall(), wantReject, command)
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	if (Command{Name: CommandRejectCall}).IsRejectCall() {
		t.Errorf("reject_call without call_id must be invalid")
	}
	if (Command{Name: CommandRejectCall, Params: map[string]string{"call_id": ""}}).IsRejectCall() {
		t.Errorf("reject_call with empty call_id must be invalid")
	}
	if (Command{Name: CommandOpenDashboard}).IsRejectCall() {
		t.Errorf("open_dashboard must not be a reject command")
	}
}
