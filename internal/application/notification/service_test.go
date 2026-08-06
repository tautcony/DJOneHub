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
	statuses  []device.Snapshot
	shownCall []CallEvent
	updated   []CallEvent
	missed    []CallEvent
	messages  []SMSMessageEvent
	offline   []DeviceOfflineEvent
	network   []NetworkUpdateEvent
	hidden    int
}

func (s *recordingSink) UpdateDeviceStatus(snapshot device.Snapshot) {
	s.record(func() { s.statuses = append(s.statuses, snapshot) })
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
func (s *recordingSink) UpdateNetwork(state NetworkUpdateEvent) {
	s.record(func() { s.network = append(s.network, state) })
}

func (s *recordingSink) record(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

func (s *recordingSink) snapshot() (shown []CallEvent, updated []CallEvent, missed []CallEvent, messages []SMSMessageEvent, offline []DeviceOfflineEvent, network []NetworkUpdateEvent, hidden int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CallEvent(nil), s.shownCall...), append([]CallEvent(nil), s.updated...), append([]CallEvent(nil), s.missed...),
		append([]SMSMessageEvent(nil), s.messages...), append([]DeviceOfflineEvent(nil), s.offline...),
		append([]NetworkUpdateEvent(nil), s.network...), s.hidden
}

// sinkState bundles the recording sink snapshot for predicate assertions.
type sinkState struct {
	shown, updated, missed []CallEvent
	messages               []SMSMessageEvent
	offline                []DeviceOfflineEvent
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
		state.shown, state.updated, state.missed, state.messages, state.offline, state.network, state.hidden = sink.snapshot()
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
	t.Cleanup(func() { _ = service.Stop(context.Background()) })
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
	return SMSMessageEvent{Index: index, Sender: sender, Body: body, ReceivedAt: received}
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
	shown, _, _, _, _, _, _ := sink.snapshot()
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
	_, _, _, messages, _, _, _ := sink.snapshot()
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
	if _, _, _, _, offlineShown, _, _ := sink.snapshot(); len(offlineShown) != 0 {
		t.Fatalf("offline must not show before threshold, shown=%v", offlineShown)
	}
	publish(t, bus, EventDeviceOffline, offline)
	waitFor(t, sink, func(state sinkState) bool { return len(state.offline) == 1 })

	// Further error events must not repeat the prompt.
	publish(t, bus, EventDeviceOffline, offline)
	time.Sleep(10 * time.Millisecond)
	if _, _, _, _, offlineShown, _, _ := sink.snapshot(); len(offlineShown) != 1 {
		t.Errorf("offline prompt must not repeat, shown=%v", offlineShown)
	}

	// Recovery allows a later prompt.
	publish(t, bus, EventDeviceStatusChanged, device.Snapshot{State: device.StateReady})
	for i := 0; i < OfflineErrorThreshold; i++ {
		publish(t, bus, EventDeviceOffline, offline)
	}
	waitFor(t, sink, func(state sinkState) bool { return len(state.offline) == 2 })
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
	if _, err := service.Debug(DebugRequest{Action: DebugSMSReceived, Body: "debug body"}); err != nil {
		t.Fatalf("debug SMS: %v", err)
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
			len(state.messages) == 1 && state.messages[0].Body == "debug body" &&
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
		{Name: CommandLog, Params: map[string]string{"level": "debug", "message": "trace"}},
	}
	for _, command := range valid {
		if err := ValidateCommand(command); err != nil {
			t.Errorf("ValidateCommand(%+v) = %v, want nil", command, err)
		}
	}
	invalid := []Command{
		{Name: CommandRejectCall},
		{Name: CommandRejectCall, Params: map[string]string{"call_id": ""}},
		{Name: CommandLog},
		{Name: CommandLog, Params: map[string]string{"level": "verbose", "message": "trace"}},
		{Name: CommandLog, Params: map[string]string{"level": "info"}},
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
	_ = service.Stop(context.Background())
	_ = service.Stop(context.Background())
}

func TestStartAndStopRepeatedly(t *testing.T) {
	bus := runtime.NewEventBus()
	sink := &recordingSink{}
	service := New(Config{Events: bus, Sink: sink})
	for i := 0; i < 3; i++ {
		if err := service.Start(context.Background()); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		_ = service.Stop(context.Background())
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("final start: %v", err)
	}
	_ = service.Stop(context.Background())
}

// gatedSink blocks every Sink call until release is closed, mimicking a
// stalled native bridge.
type gatedSink struct {
	recordingSink
	release  chan struct{}
	once     sync.Once
	first    chan struct{}
	firstRun sync.Once
}

func (s *gatedSink) gate() {
	s.firstRun.Do(func() { close(s.first) })
	s.once.Do(func() { <-s.release })
}

func (s *gatedSink) UpdateDeviceStatus(snapshot device.Snapshot) {
	s.gate()
	s.recordingSink.UpdateDeviceStatus(snapshot)
}
func (s *gatedSink) ShowCall(call CallEvent) {
	s.gate()
	s.recordingSink.ShowCall(call)
}
func (s *gatedSink) UpdateCall(call CallEvent) {
	s.gate()
	s.recordingSink.UpdateCall(call)
}
func (s *gatedSink) ShowMissedCall(call CallEvent) {
	s.gate()
	s.recordingSink.ShowMissedCall(call)
}
func (s *gatedSink) ShowSMS(message SMSMessageEvent) {
	s.gate()
	s.recordingSink.ShowSMS(message)
}
func (s *gatedSink) ShowOffline(event DeviceOfflineEvent) {
	s.gate()
	s.recordingSink.ShowOffline(event)
}
func (s *gatedSink) HideCall(call CallEvent) {
	s.gate()
	s.recordingSink.HideCall(call)
}
func (s *gatedSink) UpdateNetwork(state NetworkUpdateEvent) {
	s.gate()
	s.recordingSink.UpdateNetwork(state)
}

// mutableCallTruth is a state source whose call and SMS truth can change
// after the service has been started, for reconciliation tests. It starts
// empty so the startup baseline does not suppress later re-issued prompts.
type mutableCallTruth struct {
	mu     sync.Mutex
	active *CallEvent
	missed []CallEvent
	sms    []SMSMessageEvent
}

func (t *mutableCallTruth) setActive(call *CallEvent) {
	t.mu.Lock()
	t.active = call
	t.mu.Unlock()
}

func (t *mutableCallTruth) setMissed(calls []CallEvent, sms []SMSMessageEvent) {
	t.mu.Lock()
	t.missed = calls
	t.sms = sms
	t.mu.Unlock()
}

func (t *mutableCallTruth) snapshot(context.Context) ([]CallEvent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]CallEvent, 0, len(t.missed)+1)
	if t.active != nil {
		out = append(out, *t.active)
	}
	return append(out, t.missed...), nil
}

func (t *mutableCallTruth) smsSnapshot(context.Context) ([]SMSMessageEvent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]SMSMessageEvent(nil), t.sms...), nil
}

// stopWithTimeout bounds the deferred Stop so a failed assertion cannot hang
// the test process on a still-gated sink.
func stopWithTimeout(service *Service) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = service.Stop(ctx)
}

// floodSinkQueue publishes network events at a pace the consumer can keep up
// with, so the sink queue (not the bus) overflows deterministically; the
// delivery goroutine is stuck in the gated sink, so every enqueue beyond the
// queue capacity is counted as a sink drop.
func floodSinkQueue(t *testing.T, bus *runtime.EventBus, service *Service) {
	t.Helper()
	for i := 0; i < 2000 && service.SinkDrops() == 0; i++ {
		bus.Publish(EventNetworkUpdated, NetworkUpdateEvent{Registered: true})
		time.Sleep(time.Millisecond)
	}
	if service.SinkDrops() == 0 {
		t.Fatal("sink queue must have dropped prompts while the sink was gated")
	}
}

// publishUntilRecovered publishes recovery events and polls the sink until
// the predicate holds. Each successful enqueue after counted drops triggers
// reconciliation, so the loop converges once the queue drains; the final
// enqueue racing a still-full queue cannot stall the assertion.
func publishUntilRecovered(t *testing.T, bus *runtime.EventBus, sink *recordingSink, predicate func(state sinkState) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		bus.Publish(EventNetworkUpdated, NetworkUpdateEvent{Registered: true})
		state := sinkState{}
		state.shown, state.updated, state.missed, state.messages, state.offline, state.network, state.hidden = sink.snapshot()
		if predicate(state) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within 2s: %+v", state)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestSlowSinkDoesNotBlockEventConsumption: with the sink gated, the consumer
// keeps processing events, the sink queue counts drops, and the dropped
// prompts are re-issued after recovery (reconciliation).
func TestSlowSinkDoesNotBlockEventConsumption(t *testing.T) {
	bus := runtime.NewEventBus()
	truth := &mutableCallTruth{}
	sink := &gatedSink{release: make(chan struct{}), first: make(chan struct{})}
	service := New(Config{
		Events: bus,
		Calls:  truth.snapshot,
		SMS: func(context.Context) ([]SMSMessageEvent, error) {
			return nil, nil
		},
		Sink: sink,
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopWithTimeout(service)

	ringing := callEvent("call-1", "incoming")
	truth.setActive(&ringing)
	bus.Publish(EventCallIncoming, ringing)

	// The first sink call blocks in the gate; flood the bus so the queue
	// overflows while the consumer keeps processing events.
	floodSinkQueue(t, bus, service)

	// The call ends while the bridge is down: the hide prompt is dropped, so
	// recovery must re-derive the truth (no active call) and close the card.
	truth.setActive(nil)
	close(sink.release)
	publishUntilRecovered(t, bus, &sink.recordingSink, func(state sinkState) bool { return state.hidden >= 1 })
}

// TestReconciliationReissuesMissedCallAndSMSPromptsAfterRecovery: prompts
// dropped while the sink was gated are re-issued from the application truth
// once delivery recovers.
func TestReconciliationReissuesMissedCallAndSMSPromptsAfterRecovery(t *testing.T) {
	bus := runtime.NewEventBus()
	truth := &mutableCallTruth{}
	missed := CallEvent{ID: "call-missed", Direction: "incoming", State: "active", Number: "18900007376", Missed: true, StartedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC), NotificationEligible: true}
	ended := missed
	now := time.Now()
	ended.EndedAt = &now
	smsTruth := []SMSMessageEvent{smsEvent("10086", "reconciled message", 42)}
	sink := &gatedSink{release: make(chan struct{}), first: make(chan struct{})}
	service := New(Config{
		Events: bus,
		Calls:  truth.snapshot,
		SMS:    truth.smsSnapshot,
		Sink:   sink,
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopWithTimeout(service)

	// Flood the queue first so the sink is gated and full: the missed-call
	// and SMS prompts enqueued afterwards are dropped without being marked as
	// notified.
	floodSinkQueue(t, bus, service)
	bus.Publish(EventCallMissed, ended)
	bus.Publish(EventSMSReceived, smsTruth[0])
	time.Sleep(20 * time.Millisecond)

	// The services have since recorded the missed call and the SMS; after
	// recovery the prompts dropped from the sink queue must be re-issued
	// from that truth.
	truth.setMissed([]CallEvent{missed, ended}, smsTruth)
	close(sink.release)
	publishUntilRecovered(t, bus, &sink.recordingSink, func(state sinkState) bool {
		if len(state.missed) == 0 || state.missed[0].ID != "call-missed" {
			return false
		}
		if len(state.messages) == 0 || state.messages[0].Body != "reconciled message" {
			return false
		}
		return true
	})
}

// TestReconciliationHonorsCallNotificationEligibility covers the state-based
// recovery path that bypasses EventBus deduplication. Startup leftovers remain
// visible in call history, but they must never be forwarded to the native UI.
func TestReconciliationHonorsCallNotificationEligibility(t *testing.T) {
	bus := runtime.NewEventBus()
	truth := &mutableCallTruth{}
	sink := &recordingSink{}
	service := New(Config{Events: bus, Calls: truth.snapshot, Sink: sink})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer stopWithTimeout(service)

	startedAt := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	leftover := CallEvent{ID: "startup-leftover", Direction: "incoming", State: "incoming", StartedAt: startedAt}
	missedLeftover := CallEvent{ID: "startup-missed", Direction: "incoming", State: "incoming", StartedAt: startedAt, Missed: true}
	endedAt := startedAt.Add(time.Minute)
	missedLeftover.EndedAt = &endedAt
	truth.setActive(&leftover)
	truth.setMissed([]CallEvent{missedLeftover}, nil)

	service.reconcileCalls()
	time.Sleep(20 * time.Millisecond)
	shown, updated, missed, _, _, _, _ := sink.snapshot()
	if len(shown) != 0 || len(updated) != 0 || len(missed) != 0 {
		t.Fatalf("ineligible startup calls reached sink: shown=%v updated=%v missed=%v", shown, updated, missed)
	}

	leftover.NotificationEligible = true
	missedLeftover.NotificationEligible = true
	truth.setActive(&leftover)
	truth.setMissed([]CallEvent{missedLeftover}, nil)
	service.reconcileCalls()
	waitFor(t, sink, func(state sinkState) bool {
		return len(state.shown) == 1 && state.shown[0].ID == leftover.ID &&
			len(state.missed) == 1 && state.missed[0].ID == missedLeftover.ID
	})
}

// TestStopAbortsQueuedSinkCallsOnDeadline: a gated sink past the stop
// deadline aborts the queued prompts and Stop still joins the delivery
// goroutine before returning.
func TestStopAbortsQueuedSinkCallsOnDeadline(t *testing.T) {
	bus := runtime.NewEventBus()
	sink := &gatedSink{release: make(chan struct{}), first: make(chan struct{})}
	service := New(Config{Events: bus, Sink: sink})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bus.Publish(EventCallIncoming, callEvent("call-1", "incoming"))
	bus.Publish(EventNetworkUpdated, NetworkUpdateEvent{Registered: true})
	// Wait until the delivery goroutine is inside the gated sink call, so the
	// deadline abort path is exercised deterministically.
	select {
	case <-sink.first:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery goroutine never reached the gated sink")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := service.Stop(stopCtx); err == nil {
		t.Fatal("Stop must report the deadline error with a gated sink")
	}
	// The delivery goroutine is joined: no sink call may run after Stop.
	select {
	case <-sink.release:
		t.Fatal("release must never be consumed by a stopped service")
	default:
	}
}
