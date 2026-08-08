package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/pkg/logger"
)

// Sink receives policy-approved user prompts and menu bar model updates. The
// macOS native bridge implements it; tests use a recording fake. HideCall
// carries the ended call so the sink can correlate it with the card shown.
type Sink interface {
	UpdateDeviceStatus(snapshot device.Snapshot)
	ShowCall(call CallEvent)
	UpdateCall(call CallEvent)
	ShowMissedCall(call CallEvent)
	ShowSMS(message SMSMessageEvent)
	ShowOffline(event DeviceOfflineEvent)
	HideCall(call CallEvent)
	UpdateNetwork(state NetworkUpdateEvent)
}

// OfflineErrorThreshold freezes the legacy notifier behavior: an offline
// prompt is shown only after this many consecutive device error events.
const OfflineErrorThreshold = 5

// sinkQueueCapacity bounds how many approved prompts may wait for the native
// bridge. A permanently stalled bridge drops (and later reconciles) rather
// than growing memory; the drop is counted, never silent.
const sinkQueueCapacity = 64

// sinkOp is one queued Sink call. The delivery goroutine is the only caller of
// the Sink interface, so a slow or blocked native bridge never stalls event
// consumption. markNotified finalizes the tracked UI state on the consumer
// goroutine once the op was accepted by the queue, so reconciliation re-issues
// exactly the prompts that were dropped.
type sinkOp struct {
	name         string
	traceID      uint64
	call         func(Sink)
	markNotified func()
}

// Config injects the state sources used to baseline the policy at startup and
// to reconcile the UI after delivery recovers.
type Config struct {
	Events *runtime.EventBus
	// Calls returns the current call history including the active call, if
	// any. It must be cheap (in-memory); failures are ignored.
	Calls func(context.Context) ([]CallEvent, error)
	// SMS returns the current inbox so existing messages are not re-prompted.
	// The sms service additionally only publishes incremental messages.
	SMS func(context.Context) ([]SMSMessageEvent, error)
	// Sink receives policy-approved user prompts.
	Sink Sink
}

// Service subscribes to the shared EventBus and decides which events deserve
// a user-facing prompt. It owns the baseline and dedup keys that the legacy
// Swift notifier used to maintain, and does not depend on Swift visibility or
// the HTTP port.
type Service struct {
	config Config

	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	unsubscribe func()

	// seenCalls/seenSMS deduplicate bus events. UI delivery state (what the
	// sink has actually been told) is tracked separately and finalized only
	// when a queued op is accepted, so reconciliation can re-issue drops.
	seenCalls map[string]struct{}
	seenSMS   map[string]struct{}

	activeCallID     string
	activeCallState  string
	activeCallNumber string
	shownCall        *CallEvent
	notifiedMissed   map[string]struct{}
	notifiedSMS      map[string]struct{}

	offlineCount int
	offlineShown bool

	// sinkQueue carries approved prompts to the delivery goroutine.
	sinkQueue      chan sinkOp
	deliveryCancel context.CancelFunc
	deliveryDone   chan struct{}
	sinkDropped    atomic.Uint64
}

func New(config Config) *Service {
	return &Service{
		config:         config,
		seenCalls:      map[string]struct{}{},
		seenSMS:        map[string]struct{}{},
		notifiedMissed: map[string]struct{}{},
		notifiedSMS:    map[string]struct{}{},
	}
}

// Start baselines existing state, then subscribes to the EventBus. It is
// synchronous so the baseline is established before any event is consumed.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil
	}
	if s.config.Calls != nil {
		calls, err := s.config.Calls(ctx)
		if err == nil {
			for _, call := range calls {
				s.seenCalls[call.ID] = struct{}{}
				// Missed calls already in history are not re-prompted.
				if call.Missed && call.Direction == "incoming" {
					s.notifiedMissed[call.ID] = struct{}{}
				}
			}
		}
	}
	if s.config.SMS != nil {
		messages, err := s.config.SMS(ctx)
		if err == nil {
			for _, message := range messages {
				key := message.DedupKey()
				s.seenSMS[key] = struct{}{}
				// Existing messages are never re-prompted on recovery.
				s.notifiedSMS[key] = struct{}{}
			}
		}
	}
	_, events, unsubscribe := s.config.Events.SubscribeNamed("notification-policy", 64)
	s.unsubscribe = unsubscribe
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
	queue := make(chan sinkOp, sinkQueueCapacity)
	// The delivery context is intentionally independent of the consumer
	// context: Stop drains the queue after the consumer exits and cancels the
	// delivery context only to abort queued work past the deadline.
	sinkCtx, sinkCancel := context.WithCancel(context.Background())
	s.sinkQueue = queue
	s.deliveryCancel = sinkCancel
	deliveryDone := make(chan struct{})
	s.deliveryDone = deliveryDone
	go func() {
		defer unsubscribe()
		defer close(done)
		for {
			select {
			case <-runCtx.Done():
				return
			case event := <-events:
				s.handle(event)
			}
		}
	}()
	go func() {
		defer close(deliveryDone)
		for {
			select {
			case <-sinkCtx.Done():
				return
			case op, ok := <-queue:
				if !ok {
					return
				}
				s.config.Events.RecordTraceHop(op.traceID, "notification-queue", "dequeue", "success", op.name)
				op.call(s.config.Sink)
				s.config.Events.RecordTraceHop(op.traceID, "native-ui", "dispatch", "success", op.name)
			}
		}
	}()
	return nil
}

// Stop stops the consumer, drains the sink queue until the deadline, and
// waits for the delivery goroutine. It is safe to call multiple times and
// after Start was never called. When ctx expires, the queued sink calls are
// aborted and the delivery goroutine is still joined before returning, so the
// native UI is never stopped underneath a live sink call.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	unsubscribe := s.unsubscribe
	s.cancel = nil
	s.done = nil
	s.unsubscribe = nil
	s.mu.Unlock()
	if cancel == nil {
		// Never started (or already stopped): nothing to join.
		if unsubscribe != nil {
			unsubscribe()
		}
		return nil
	}
	cancel()
	if done != nil {
		<-done
	}
	s.mu.Lock()
	queue := s.sinkQueue
	deliveryCancel := s.deliveryCancel
	deliveryDone := s.deliveryDone
	s.sinkQueue = nil
	s.deliveryDone = nil
	s.mu.Unlock()
	if queue == nil || deliveryDone == nil {
		return nil
	}
	// The consumer has exited, so no new ops are enqueued. Closing the queue
	// lets the delivery goroutine drain what is queued and exit; if the
	// deadline expires first, its context is cancelled to abort the rest.
	close(queue)
	select {
	case <-deliveryDone:
		return nil
	case <-ctx.Done():
		// Deadline expired: abort the queued work so the delivery goroutine
		// stops consuming and exits once its in-flight sink call returns.
		// Stop returns the deadline error without joining the goroutine, so a
		// permanently stuck bridge call cannot hang shutdown.
		if deliveryCancel != nil {
			deliveryCancel()
		}
		return ctx.Err()
	}
}

// SinkDrops reports the number of prompts dropped because the sink queue was
// full, for tests and diagnostics.
func (s *Service) SinkDrops() uint64 { return s.sinkDropped.Load() }

type Diagnostics struct {
	Running       bool   `json:"running"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
	Dropped       uint64 `json:"dropped"`
}

func (s *Service) Diagnostics() Diagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()
	depth, capacity := 0, 0
	if s.sinkQueue != nil {
		depth, capacity = len(s.sinkQueue), cap(s.sinkQueue)
	}
	return Diagnostics{
		Running: s.cancel != nil, QueueDepth: depth,
		QueueCapacity: capacity, Dropped: s.sinkDropped.Load(),
	}
}

// enqueueSink hands an approved prompt to the delivery goroutine without
// blocking. On a full queue the prompt is dropped and counted; the first
// successful enqueue afterwards triggers reconciliation so the UI converges on
// the true device state instead of staying stuck on a dropped prompt.
func (s *Service) enqueueSink(op sinkOp) {
	select {
	case s.sinkQueue <- op:
		s.config.Events.RecordTraceHop(op.traceID, "notification-queue", "enqueue", "success", op.name)
		if strings.Contains(op.name, "call") {
			logger.Info("[notification] sink enqueue", "name", op.name)
		}
		if op.markNotified != nil {
			op.markNotified()
		}
		if s.sinkDropped.Load() > 0 {
			s.reconcile()
		}
	default:
		// A slow bridge must not block event consumption; the drop is
		// counted and reconciled once delivery recovers.
		s.sinkDropped.Add(1)
		s.config.Events.RecordTraceHop(op.traceID, "notification-queue", "enqueue", "dropped", "queue full")
		logger.Warn("[notification] sink drop", "name", op.name, "reason", "queue_full")
	}
}

func (s *Service) handle(event runtime.Event) {
	s.config.Events.RecordTraceHop(event.ID, "notification-policy", "handle", "success", event.Type)
	switch event.Type {
	case EventCallIncoming, EventCallUpdated:
		if call, ok := event.Data.(CallEvent); ok {
			call.TraceID = event.ID
			logger.Info("[notification] received call event", "event", event.Type, "call_id", call.ID, "direction", call.Direction, "state", call.State, "number", call.Number)
			s.applyCall(call)
		}
	case EventCallEnded, EventCallMissed:
		if call, ok := event.Data.(CallEvent); ok {
			call.TraceID = event.ID
			logger.Info("[notification] received call end event", "event", event.Type, "call_id", call.ID, "direction", call.Direction, "state", call.State, "missed", call.Missed)
			s.applyCallEnd(call)
		}
	case EventSMSReceived:
		if message, ok := event.Data.(SMSMessageEvent); ok {
			message.TraceID = event.ID
			s.applySMS(message)
		}
	case EventDeviceStatusChanged:
		if snapshot, ok := event.Data.(device.Snapshot); ok {
			s.enqueueSink(sinkOp{name: "update_device_status", traceID: event.ID, call: func(sink Sink) { sink.UpdateDeviceStatus(snapshot) }})
			s.applyDeviceState(snapshot.State, nil)
		}
	case EventDeviceOffline:
		if offline, ok := event.Data.(device.OfflineEvent); ok {
			prompt := DeviceOfflineEvent{TraceID: event.ID, State: string(offline.State), Reason: offline.Reason, LastError: offline.LastError}
			s.applyDeviceState(offline.State, &prompt)
		}
	case EventNetworkUpdated:
		if state, ok := event.Data.(NetworkUpdateEvent); ok {
			state.TraceID = event.ID
			s.enqueueSink(sinkOp{name: "update_network", traceID: event.ID, call: func(sink Sink) { sink.UpdateNetwork(state) }})
		}
	}
}

func (s *Service) applyCall(call CallEvent) {
	var op *sinkOp
	s.mu.Lock()
	if _, seen := s.seenCalls[call.ID]; seen {
		if call.ID == s.activeCallID && (call.State != s.activeCallState || call.Number != s.activeCallNumber) {
			c := call
			op = &sinkOp{
				name:    "update_call",
				traceID: c.TraceID,
				call:    func(sink Sink) { sink.UpdateCall(c) },
				markNotified: func() {
					s.activeCallState = c.State
					s.activeCallNumber = c.Number
				},
			}
			logger.Info("[notification] queue update_call", "call_id", call.ID, "state", call.State, "number", call.Number)
		}
	} else {
		s.seenCalls[call.ID] = struct{}{}
		if call.Direction == "incoming" && (call.State == "incoming" || call.State == "waiting") {
			c := call
			op = &sinkOp{
				name:    "show_call",
				traceID: c.TraceID,
				call:    func(sink Sink) { sink.ShowCall(c) },
				markNotified: func() {
					s.activeCallID = c.ID
					s.activeCallState = c.State
					s.activeCallNumber = c.Number
					s.shownCall = &c
				},
			}
			logger.Info("[notification] queue show_call", "call_id", call.ID, "state", call.State, "number", call.Number)
		} else {
			logger.Info("[notification] suppress new call", "call_id", call.ID, "direction", call.Direction, "state", call.State, "reason", "not_incoming_state")
		}
		// Calls that arrive already active (answered before the poller saw the
		// ringing state) are recorded but do not interrupt the user.
	}
	s.mu.Unlock()
	if op != nil {
		s.enqueueSink(*op)
	}
}

func (s *Service) applyCallEnd(call CallEvent) {
	var ops []sinkOp
	s.mu.Lock()
	s.seenCalls[call.ID] = struct{}{}
	if call.ID == s.activeCallID {
		if shown := s.shownCall; shown != nil {
			hidden := *shown
			ops = append(ops, sinkOp{
				name:    "hide_call",
				traceID: call.TraceID,
				call:    func(sink Sink) { sink.HideCall(hidden) },
				markNotified: func() {
					s.activeCallID = ""
					s.activeCallState = ""
					s.activeCallNumber = ""
					s.shownCall = nil
				},
			})
		} else {
			// No card is tracked as shown; nothing to hide, but the tracked
			// state is cleared so reconciliation starts from a clean slate.
			s.activeCallID = ""
			s.activeCallState = ""
			s.activeCallNumber = ""
		}
	}
	if call.Missed && call.Direction == "incoming" {
		c := call
		logger.Info("[notification] queue show_missed_call", "call_id", call.ID, "number", call.Number)
		ops = append(ops, sinkOp{
			name:    "show_missed_call",
			traceID: c.TraceID,
			call:    func(sink Sink) { sink.ShowMissedCall(c) },
			markNotified: func() {
				s.notifiedMissed[c.ID] = struct{}{}
			},
		})
	}
	s.mu.Unlock()
	for _, op := range ops {
		s.enqueueSink(op)
	}
}

func (s *Service) applySMS(message SMSMessageEvent) {
	var op *sinkOp
	s.mu.Lock()
	key := message.DedupKey()
	if _, seen := s.seenSMS[key]; seen {
		s.mu.Unlock()
		return
	}
	s.seenSMS[key] = struct{}{}
	m := message
	op = &sinkOp{
		name:    "show_sms",
		traceID: m.TraceID,
		call:    func(sink Sink) { sink.ShowSMS(m) },
		markNotified: func() {
			s.notifiedSMS[m.DedupKey()] = struct{}{}
		},
	}
	s.mu.Unlock()
	if op != nil {
		s.enqueueSink(*op)
	}
}

// applyDeviceState counts offline signals; recovery resets the streak. The
// optional prompt carries the offline detail when the event itself was a
// device.offline publication.
func (s *Service) applyDeviceState(state device.State, offline *DeviceOfflineEvent) {
	var op *sinkOp
	s.mu.Lock()
	if state == device.StateReady {
		s.offlineCount = 0
		s.offlineShown = false
		s.mu.Unlock()
		return
	}
	if !isOfflineState(state) {
		s.mu.Unlock()
		return
	}
	s.offlineCount++
	if s.offlineCount >= OfflineErrorThreshold && !s.offlineShown && offline != nil {
		s.offlineShown = true
		e := *offline
		op = &sinkOp{name: "show_offline", traceID: e.TraceID, call: func(sink Sink) { sink.ShowOffline(e) }}
	}
	s.mu.Unlock()
	if op != nil {
		s.enqueueSink(*op)
	}
}

func isOfflineState(state device.State) bool {
	switch state {
	case device.StateAbsent, device.StateDisconnected, device.StateDegraded:
		return true
	}
	return false
}

// reconcile re-derives the call and SMS state from the application services
// and re-issues the prompts that were dropped, so the UI converges on the
// true device state (e.g. the incoming-call card closes after a dropped
// hangup). It runs on the consumer goroutine; every re-issue goes through the
// delivery queue with the same markNotified discipline.
func (s *Service) reconcile() {
	s.sinkDropped.Store(0)
	s.reconcileCalls()
	s.reconcileSMS()
}

func (s *Service) reconcileCalls() {
	if s.config.Calls == nil {
		return
	}
	calls, err := s.config.Calls(context.Background())
	if err != nil {
		return
	}
	var active *CallEvent
	for i := range calls {
		if calls[i].EndedAt == nil {
			call := calls[i]
			active = &call
			break
		}
	}
	var ops []sinkOp
	s.mu.Lock()
	switch {
	case active == nil:
		if shown := s.shownCall; shown != nil {
			hidden := *shown
			ops = append(ops, sinkOp{
				name: "reconcile_hide_call",
				call: func(sink Sink) { sink.HideCall(hidden) },
				markNotified: func() {
					s.activeCallID = ""
					s.activeCallState = ""
					s.activeCallNumber = ""
					s.shownCall = nil
				},
			})
		}
	case s.shownCall == nil:
		if isReconciledRingingCall(active) {
			c := *active
			ops = append(ops, sinkOp{
				name: "reconcile_show_call",
				call: func(sink Sink) { sink.ShowCall(c) },
				markNotified: func() {
					s.activeCallID = c.ID
					s.activeCallState = c.State
					s.activeCallNumber = c.Number
					s.shownCall = &c
				},
			})
		} else if !active.NotificationEligible {
			logger.Info("[notification] suppress reconciled active call", "call_id", active.ID, "state", active.State, "reason", "notification_ineligible")
		}
	default:
		if s.shownCall.ID != active.ID {
			hidden := *s.shownCall
			ops = append(ops, sinkOp{
				name: "reconcile_hide_call",
				call: func(sink Sink) { sink.HideCall(hidden) },
				markNotified: func() {
					s.activeCallID = ""
					s.activeCallState = ""
					s.activeCallNumber = ""
					s.shownCall = nil
				},
			})
			if isReconciledRingingCall(active) {
				c := *active
				ops = append(ops, sinkOp{
					name: "reconcile_show_call",
					call: func(sink Sink) { sink.ShowCall(c) },
					markNotified: func() {
						s.activeCallID = c.ID
						s.activeCallState = c.State
						s.activeCallNumber = c.Number
						s.shownCall = &c
					},
				})
			}
		} else if active.NotificationEligible && (active.State != s.activeCallState || active.Number != s.activeCallNumber) {
			c := *active
			ops = append(ops, sinkOp{
				name: "reconcile_update_call",
				call: func(sink Sink) { sink.UpdateCall(c) },
				markNotified: func() {
					s.activeCallState = c.State
					s.activeCallNumber = c.Number
				},
			})
		}
	}
	// Re-issue missed-call prompts that were never delivered.
	for i := range calls {
		call := calls[i]
		if call.EndedAt == nil || !call.Missed || call.Direction != "incoming" {
			continue
		}
		if !call.NotificationEligible {
			logger.Info("[notification] suppress reconciled missed call", "call_id", call.ID, "reason", "notification_ineligible")
			continue
		}
		if _, notified := s.notifiedMissed[call.ID]; notified {
			continue
		}
		c := call
		ops = append(ops, sinkOp{
			name: "reconcile_show_missed_call",
			call: func(sink Sink) { sink.ShowMissedCall(c) },
			markNotified: func() {
				s.notifiedMissed[c.ID] = struct{}{}
			},
		})
	}
	s.mu.Unlock()
	for _, op := range ops {
		s.enqueueSink(op)
	}
}

func isReconciledRingingCall(call *CallEvent) bool {
	return call != nil && call.NotificationEligible && call.Direction == "incoming" &&
		(call.State == "incoming" || call.State == "waiting")
}

func (s *Service) reconcileSMS() {
	if s.config.SMS == nil {
		return
	}
	messages, err := s.config.SMS(context.Background())
	if err != nil {
		return
	}
	var ops []sinkOp
	s.mu.Lock()
	for _, message := range messages {
		key := message.DedupKey()
		if _, notified := s.notifiedSMS[key]; notified {
			continue
		}
		m := message
		ops = append(ops, sinkOp{
			name: "reconcile_show_sms",
			call: func(sink Sink) { sink.ShowSMS(m) },
			markNotified: func() {
				s.notifiedSMS[m.DedupKey()] = struct{}{}
			},
		})
	}
	s.mu.Unlock()
	for _, op := range ops {
		s.enqueueSink(op)
	}
}

// Debug publishes one of the supported notifier scenarios through the same
// EventBus consumed by the notification policy and the WebSocket bridge.
// Defaults are intentionally deterministic enough for a human to recognize
// the card, while timestamps and generated call IDs keep repeated tests from
// being swallowed by deduplication.
func (s *Service) Debug(request DebugRequest) ([]runtime.Event, error) {
	if s.config.Events == nil {
		return nil, fmt.Errorf("notification event bus is unavailable")
	}

	action := strings.TrimSpace(request.Action)
	now := time.Now().UTC()
	switch action {
	case DebugCallIncoming:
		call, err := debugCall(request, now, false, false)
		if err != nil {
			return nil, err
		}
		return []runtime.Event{s.config.Events.Publish(EventCallIncoming, call)}, nil
	case DebugCallUpdated:
		call, err := debugCall(request, now, false, true)
		if err != nil {
			return nil, err
		}
		return []runtime.Event{s.config.Events.Publish(EventCallUpdated, call)}, nil
	case DebugCallEnded:
		call, err := debugCall(request, now, true, false)
		if err != nil {
			return nil, err
		}
		return []runtime.Event{s.config.Events.Publish(EventCallEnded, call)}, nil
	case DebugCallMissed:
		call, err := debugCall(request, now, true, false)
		if err != nil {
			return nil, err
		}
		call.Missed = true
		return []runtime.Event{s.config.Events.Publish(EventCallMissed, call)}, nil
	case DebugSMSReceived:
		message := SMSMessageEvent{
			Index:      9000,
			Sender:     firstNonEmpty(request.Sender, "10086"),
			Recipient:  strings.TrimSpace(request.Recipient),
			Body:       firstNonEmpty(request.Body, "DJOneHubNotifier debug message"),
			ReceivedAt: now,
		}
		return []runtime.Event{s.config.Events.Publish(EventSMSReceived, message)}, nil
	case DebugDeviceOffline:
		offline := device.OfflineEvent{
			State:     device.StateDisconnected,
			Reason:    "debug notification: device offline",
			LastError: "debug notification: device offline",
		}
		events := make([]runtime.Event, 0, OfflineErrorThreshold)
		for i := 0; i < OfflineErrorThreshold; i++ {
			events = append(events, s.config.Events.Publish(EventDeviceOffline, offline))
		}
		return events, nil
	case DebugDeviceReady:
		return []runtime.Event{s.config.Events.Publish(EventDeviceStatusChanged, device.Snapshot{State: device.StateReady})}, nil
	case DebugNetworkConnected:
		return []runtime.Event{s.config.Events.Publish(EventNetworkUpdated, NetworkUpdateEvent{
			Mode: "usbnet", NetworkMode: "LTE", Registered: true, Operator: "DJOneHub Debug", SignalDBM: -71,
		})}, nil
	case DebugNetworkWeak:
		return []runtime.Event{s.config.Events.Publish(EventNetworkUpdated, NetworkUpdateEvent{
			Mode: "usbnet", NetworkMode: "LTE", Registered: true, Operator: "DJOneHub Debug", SignalDBM: -101,
		})}, nil
	case DebugNetworkOffline:
		return []runtime.Event{s.config.Events.Publish(EventNetworkUpdated, NetworkUpdateEvent{NetworkMode: "LTE", Registered: false})}, nil
	default:
		return nil, fmt.Errorf("unsupported notification debug action %q", action)
	}
}

func debugCall(request DebugRequest, now time.Time, ended bool, updated bool) (CallEvent, error) {
	callID := strings.TrimSpace(request.CallID)
	if callID == "" && !ended && !updated {
		callID = fmt.Sprintf("debug-call-%d", now.UnixNano())
	}
	if callID == "" {
		return CallEvent{}, fmt.Errorf("call_id is required for this notification debug action")
	}
	state := "incoming"
	if updated {
		state = "active"
	}
	call := CallEvent{
		ID:        callID,
		Direction: "incoming",
		State:     state,
		Number:    firstNonEmpty(request.Number, "13800138000"),
		StartedAt: now,
	}
	if ended {
		call.EndedAt = &now
	}
	return call, nil
}

func firstNonEmpty(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

// ValidateCommand validates a native command before the bridge executes it.
func ValidateCommand(command Command) error {
	switch command.Name {
	case CommandRejectCall:
		if command.CallID() == "" {
			return fmt.Errorf("reject_call requires a call_id parameter")
		}
	case CommandOpenDashboard:
		// No parameters are expected.
	case CommandNotificationPermissionStatus:
		if !ValidNotificationPermissionState(command.Param("state")) {
			return fmt.Errorf("notification_permission_status requires a valid state")
		}
	case CommandLog:
		if !ValidNativeLogLevel(command.Param("level")) {
			return fmt.Errorf("log requires a valid level")
		}
		if command.Param("message") == "" {
			return fmt.Errorf("log requires a message")
		}
	default:
		return fmt.Errorf("unknown native command %q", command.Name)
	}
	return nil
}
