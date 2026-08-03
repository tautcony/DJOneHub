package notification

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

type recordingSink struct {
	mu        sync.Mutex
	shownCall []CallEvent
	updated   []CallEvent
	missed    []CallEvent
	messages  []SMSMessageEvent
	offline   []DeviceOfflineEvent
	gps       []GPSUpdateEvent
	network   []NetworkUpdateEvent
	hidden    int
}

func (s *recordingSink) ShowCall(call CallEvent) {
	s.record(func() { s.shownCall = append(s.shownCall, call) })
}
func (s *recordingSink) UpdateCall(call CallEvent) {
	s.record(func() { s.updated = append(s.updated, call) })
}
func (s *recordingSink) ShowMissedCall(call CallEvent) {
	s.record(func() { s.missed = append(s.missed, call) })
}
func (s *recordingSink) ShowSMS(message SMSMessageEvent) {
	s.record(func() { s.messages = append(s.messages, message) })
}
func (s *recordingSink) ShowOffline(event DeviceOfflineEvent) {
	s.record(func() { s.offline = append(s.offline, event) })
}
func (s *recordingSink) HideCall(call CallEvent) { s.record(func() { s.hidden++ }) }
func (s *recordingSink) UpdateGPS(status GPSUpdateEvent) {
	s.record(func() { s.gps = append(s.gps, status) })
}
func (s *recordingSink) UpdateNetwork(state NetworkUpdateEvent) {
	s.record(func() { s.network = append(s.network, state) })
}

func (s *recordingSink) record(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

func (s *recordingSink) snapshot() (shown []CallEvent, updated []CallEvent, missed []CallEvent, messages []SMSMessageEvent, offline []DeviceOfflineEvent, gps []GPSUpdateEvent, network []NetworkUpdateEvent, hidden int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CallEvent(nil), s.shownCall...), append([]CallEvent(nil), s.updated...), append([]CallEvent(nil), s.missed...),
		append([]SMSMessageEvent(nil), s.messages...), append([]DeviceOfflineEvent(nil), s.offline...),
		append([]GPSUpdateEvent(nil), s.gps...), append([]NetworkUpdateEvent(nil), s.network...), s.hidden
}

// sinkState bundles the recording sink snapshot for predicate assertions.
type sinkState struct {
	shown, updated, missed []CallEvent
	messages               []SMSMessageEvent
	offline                []DeviceOfflineEvent
	gps                    []GPSUpdateEvent
	network                []NetworkUpdateEvent
	hidden                 int
}

// waitFor polls the sink until the predicate holds; events are consumed by a
// background goroutine, so assertions must not read the sink synchronously.
func waitFor(t *testing.T, sink *recordingSink, predicate func(state sinkState) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		state := sinkState{}
		state.shown, state.updated, state.missed, state.messages, state.offline, state.gps, state.network, state.hidden = sink.snapshot()
		if predicate(state) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within 2s: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newService(t *testing.T, bus *runtime.EventBus, calls []CallEvent, sms []SMSMessageEvent) (*Service, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	service := New(Config{
		Events: bus,
		Calls: func(context.Context) ([]CallEvent, error) {
			return append([]CallEvent(nil), calls...), nil
		},
		SMS: func(context.Context) ([]SMSMessageEvent, error) {
			return append([]SMSMessageEvent(nil), sms...), nil
		},
		Sink: sink,
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(service.Stop)
	return service, sink
}

func publish(t *testing.T, bus *runtime.EventBus, eventType string, data any) {
	t.Helper()
	bus.Publish(eventType, data)
}

func callEvent(id, state string) CallEvent {
	return CallEvent{ID: id, Direction: "incoming", State: state, Number: "18900007376", StartedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)}
}

func smsEvent(sender, body string, index int) SMSMessageEvent {
	received := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	return SMSMessageEvent{Index: index, Sender: sender, Body: body, Code: "482913", ReceivedAt: received}
}

func TestStartupBaselineSuppressesExistingState(t *testing.T) {
	bus := runtime.NewEventBus()
	existing := []CallEvent{callEvent("hist-1", "active"), callEvent("hist-2", "incoming")}
	messages := []SMSMessageEvent{smsEvent("10086", "旧的验证码 1111", 1), smsEvent("10010", "旧消息", 2)}
	_, sink := newService(t, bus, existing, messages)

	publish(t, bus, EventCallIncoming, existing[1])
	publish(t, bus, EventSMSReceived, messages[0])
	publish(t, bus, EventCallEnded, callEvent("hist-1", "active"))

	// Baseline calls were never shown as cards, so nothing to hide.
	waitFor(t, sink, func(state sinkState) bool {
		return len(state.shown) == 0 && len(state.messages) == 0 && state.hidden == 0
	})

	// New state after baseline still prompts.
	publish(t, bus, EventCallIncoming, callEvent("fresh-1", "incoming"))
	publish(t, bus, EventSMSReceived, smsEvent("10086", "新的验证码 9999", 3))
	waitFor(t, sink, func(state sinkState) bool {
		return len(state.shown) == 1 && state.shown[0].ID == "fresh-1" &&
			len(state.messages) == 1 && state.messages[0].Index == 3
	})
}

func TestIncomingCallShownOnceAndUpdated(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	publish(t, bus, EventCallIncoming, callEvent("call-1", "incoming"))
	publish(t, bus, EventCallIncoming, callEvent("call-1", "incoming"))
	publish(t, bus, EventCallUpdated, callEvent("call-1", "active"))

	waitFor(t, sink, func(state sinkState) bool {
		return len(state.shown) == 1 && state.shown[0].ID == "call-1" &&
			len(state.updated) == 1 && state.updated[0].State == "active"
	})
}

func TestIncomingCallNumberUpdateIsForwarded(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	publish(t, bus, EventCallIncoming, callEvent("call-1", "incoming"))
	updated := callEvent("call-1", "incoming")
	updated.Number = "10010"
	publish(t, bus, EventCallUpdated, updated)

	waitFor(t, sink, func(state sinkState) bool {
		return len(state.updated) == 1 && state.updated[0].Number == "10010"
	})
}

func TestCallEndedHidesCard(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	publish(t, bus, EventCallIncoming, callEvent("call-1", "incoming"))
	ended := callEvent("call-1", "incoming")
	now := time.Date(2026, 8, 2, 10, 1, 0, 0, time.UTC)
	ended.EndedAt = &now
	publish(t, bus, EventCallEnded, ended)

	waitFor(t, sink, func(state sinkState) bool {
		return state.hidden == 1 && len(state.missed) == 0
	})
}

func TestMissedCallPrompts(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	publish(t, bus, EventCallIncoming, callEvent("call-1", "incoming"))
	missed := callEvent("call-1", "incoming")
	missed.Missed = true
	now := time.Date(2026, 8, 2, 10, 0, 45, 0, time.UTC)
	missed.EndedAt = &now
	publish(t, bus, EventCallMissed, missed)

	waitFor(t, sink, func(state sinkState) bool {
		return state.hidden == 1 && len(state.missed) == 1 && state.missed[0].ID == "call-1"
	})
}

func TestOutgoingAndAnsweredCallsDoNotPrompt(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	outgoing := callEvent("call-2", "dialing")
	outgoing.Direction = "outgoing"
	publish(t, bus, EventCallIncoming, outgoing)
	publish(t, bus, EventCallIncoming, callEvent("call-3", "active"))

	time.Sleep(10 * time.Millisecond)
	shown, _, _, _, _, _, _, _ := sink.snapshot()
	if len(shown) != 0 {
		t.Errorf("non-ringing calls must not prompt, shown=%v", shown)
	}
}

func TestSMSDedup(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	message := smsEvent("10086", "您的验证码是 482913", 7)
	publish(t, bus, EventSMSReceived, message)
	publish(t, bus, EventSMSReceived, message)

	waitFor(t, sink, func(state sinkState) bool { return len(state.messages) == 1 })
	time.Sleep(10 * time.Millisecond)
	_, _, _, messages, _, _, _, _ := sink.snapshot()
	if len(messages) != 1 {
		t.Errorf("duplicate sms must prompt once, messages=%v", messages)
	}
}

func TestOfflineThresholdAndRecovery(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	offline := device.OfflineEvent{State: device.StateDisconnected, Reason: "no managed device was discovered", LastError: "no managed device was discovered"}
	for i := 0; i < OfflineErrorThreshold-1; i++ {
		publish(t, bus, EventDeviceOffline, offline)
	}
	time.Sleep(10 * time.Millisecond)
	if _, _, _, _, offlineShown, _, _, _ := sink.snapshot(); len(offlineShown) != 0 {
		t.Fatalf("offline must not show before threshold, shown=%v", offlineShown)
	}
	publish(t, bus, EventDeviceOffline, offline)
	waitFor(t, sink, func(state sinkState) bool { return len(state.offline) == 1 })

	// Further error events must not repeat the prompt.
	publish(t, bus, EventDeviceOffline, offline)
	time.Sleep(10 * time.Millisecond)
	if _, _, _, _, offlineShown, _, _, _ := sink.snapshot(); len(offlineShown) != 1 {
		t.Errorf("offline prompt must not repeat, shown=%v", offlineShown)
	}

	// Recovery allows a later prompt.
	publish(t, bus, EventDeviceStatusChanged, device.Snapshot{State: device.StateReady})
	for i := 0; i < OfflineErrorThreshold; i++ {
		publish(t, bus, EventDeviceOffline, offline)
	}
	waitFor(t, sink, func(state sinkState) bool { return len(state.offline) == 2 })
}

func TestGPSAndNetworkForwardToMenuBarModels(t *testing.T) {
	bus := runtime.NewEventBus()
	_, sink := newService(t, bus, nil, nil)

	gps := GPSUpdateEvent{Enabled: true, Fix: &GPSFixEvent{Latitude: "31.2304", Longitude: "121.4737", HDOP: "1.1", Satellites: "12"}, LastChecked: time.Now().UTC()}
	state := NetworkUpdateEvent{NetworkMode: "LTE", Registered: true, SignalDBM: -83}
	publish(t, bus, EventGPSUpdated, gps)
	publish(t, bus, EventNetworkUpdated, state)

	waitFor(t, sink, func(state sinkState) bool {
		return len(state.gps) == 1 && state.gps[0].Fix.Latitude == "31.2304" &&
			len(state.network) == 1 && state.network[0].SignalDBM == -83
	})
}

func TestDebugPublishesNotifierScenarios(t *testing.T) {
	bus := runtime.NewEventBus()
	service, sink := newService(t, bus, nil, nil)

	events, err := service.Debug(DebugRequest{Action: DebugCallIncoming, Number: "10010"})
	if err != nil || len(events) != 1 {
		t.Fatalf("debug incoming = %v, events=%d", err, len(events))
	}
	call, ok := events[0].Data.(CallEvent)
	if !ok || call.ID == "" {
		t.Fatalf("generated call event = %#v", events[0].Data)
	}
	callID := call.ID

	if _, err := service.Debug(DebugRequest{Action: DebugCallUpdated, CallID: callID}); err != nil {
		t.Fatalf("debug updated: %v", err)
	}
	if _, err := service.Debug(DebugRequest{Action: DebugCallMissed, CallID: callID}); err != nil {
		t.Fatalf("debug missed: %v", err)
	}
	if _, err := service.Debug(DebugRequest{Action: DebugSMSReceived, Body: "debug body", Code: "123456"}); err != nil {
		t.Fatalf("debug SMS: %v", err)
	}
	if _, err := service.Debug(DebugRequest{Action: DebugGPSFix}); err != nil {
		t.Fatalf("debug GPS: %v", err)
	}
	if _, err := service.Debug(DebugRequest{Action: DebugNetworkWeak}); err != nil {
		t.Fatalf("debug network: %v", err)
	}
	if events, err := service.Debug(DebugRequest{Action: DebugDeviceOffline}); err != nil || len(events) != OfflineErrorThreshold {
		t.Fatalf("debug offline = %v, events=%d", err, len(events))
	}

	waitFor(t, sink, func(state sinkState) bool {
		return len(state.shown) == 1 && state.shown[0].ID == callID &&
			len(state.updated) == 1 && state.updated[0].ID == callID &&
			len(state.missed) == 1 && state.missed[0].ID == callID &&
			len(state.messages) == 1 && state.messages[0].Code == "123456" &&
			len(state.gps) == 1 && state.gps[0].Fix != nil &&
			len(state.network) == 1 && state.network[0].SignalDBM == -101 &&
			len(state.offline) == 1 && state.hidden == 1
	})
}

func TestDebugRequiresCallIDForCallLifecycleContinuation(t *testing.T) {
	service := New(Config{Events: runtime.NewEventBus()})
	if _, err := service.Debug(DebugRequest{Action: DebugCallUpdated}); err == nil {
		t.Fatal("updated debug action without call_id must fail")
	}
	if _, err := service.Debug(DebugRequest{Action: "unknown"}); err == nil {
		t.Fatal("unknown debug action must fail")
	}
}

func TestValidateCommand(t *testing.T) {
	valid := []Command{
		{Name: CommandRejectCall, Params: map[string]string{"call_id": "call-1"}},
		{Name: CommandOpenDashboard},
		{Name: CommandToggleGPSPanel},
		{Name: CommandOpenGPSPanel},
		{Name: CommandCloseGPSPanel},
	}
	for _, command := range valid {
		if err := ValidateCommand(command); err != nil {
			t.Errorf("ValidateCommand(%+v) = %v, want nil", command, err)
		}
	}
	invalid := []Command{
		{Name: CommandRejectCall},
		{Name: CommandRejectCall, Params: map[string]string{"call_id": ""}},
		{Name: "unknown"},
	}
	for _, command := range invalid {
		if err := ValidateCommand(command); err == nil {
			t.Errorf("ValidateCommand(%+v) must fail", command)
		}
	}
}

func TestStopWithoutStartIsSafe(t *testing.T) {
	service := New(Config{})
	service.Stop()
	service.Stop()
}

func TestStartAndStopRepeatedly(t *testing.T) {
	bus := runtime.NewEventBus()
	sink := &recordingSink{}
	service := New(Config{Events: bus, Sink: sink})
	for i := 0; i < 3; i++ {
		if err := service.Start(context.Background()); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		service.Stop()
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("final start: %v", err)
	}
	service.Stop()
}
