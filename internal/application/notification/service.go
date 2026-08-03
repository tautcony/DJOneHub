package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

// Sink receives policy-approved user prompts and menu bar model updates. The
// macOS native bridge implements it; tests use a recording fake. HideCall
// carries the ended call so the sink can correlate it with the card shown.
type Sink interface {
	ShowCall(call CallEvent)
	UpdateCall(call CallEvent)
	ShowMissedCall(call CallEvent)
	ShowSMS(message SMSMessageEvent)
	ShowOffline(event DeviceOfflineEvent)
	HideCall(call CallEvent)
	UpdateGPS(status GPSUpdateEvent)
	UpdateNetwork(state NetworkUpdateEvent)
}

// OfflineErrorThreshold freezes the legacy notifier behavior: an offline
// prompt is shown only after this many consecutive device error events.
const OfflineErrorThreshold = 5

// Config injects the state sources used to baseline the policy at startup.
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

	seenCalls        map[string]struct{}
	activeCallID     string
	activeCallState  string
	activeCallNumber string
	seenSMS          map[string]struct{}
	offlineCount     int
	offlineShown     bool
}

func New(config Config) *Service {
	return &Service{
		config:    config,
		seenCalls: map[string]struct{}{},
		seenSMS:   map[string]struct{}{},
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
			}
		}
	}
	if s.config.SMS != nil {
		messages, err := s.config.SMS(ctx)
		if err == nil {
			for _, message := range messages {
				s.seenSMS[message.DedupKey()] = struct{}{}
			}
		}
	}
	_, events, unsubscribe := s.config.Events.Subscribe(64)
	s.unsubscribe = unsubscribe
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
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
	return nil
}

// Stop unsubscribes and waits for the consumer goroutine to exit. It is safe
// to call multiple times and after Start was never called.
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	unsubscribe := s.unsubscribe
	s.cancel = nil
	s.done = nil
	s.unsubscribe = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		if done != nil {
			<-done
		}
	} else if unsubscribe != nil {
		unsubscribe()
	}
}

func (s *Service) handle(event runtime.Event) {
	switch event.Type {
	case EventCallIncoming, EventCallUpdated:
		if call, ok := event.Data.(CallEvent); ok {
			s.applyCall(call)
		}
	case EventCallEnded, EventCallMissed:
		if call, ok := event.Data.(CallEvent); ok {
			s.applyCallEnd(call)
		}
	case EventSMSReceived:
		if message, ok := event.Data.(SMSMessageEvent); ok {
			s.applySMS(message)
		}
	case EventDeviceStatusChanged:
		if snapshot, ok := event.Data.(device.Snapshot); ok {
			s.applyDeviceState(snapshot.State, nil)
		}
	case EventDeviceOffline:
		if offline, ok := event.Data.(device.OfflineEvent); ok {
			prompt := DeviceOfflineEvent{State: string(offline.State), Reason: offline.Reason, LastError: offline.LastError}
			s.applyDeviceState(offline.State, &prompt)
		}
	case EventGPSUpdated:
		if status, ok := event.Data.(GPSUpdateEvent); ok {
			s.config.Sink.UpdateGPS(status)
		}
	case EventNetworkUpdated:
		if state, ok := event.Data.(NetworkUpdateEvent); ok {
			s.config.Sink.UpdateNetwork(state)
		}
	}
}

func (s *Service) applyCall(call CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.seenCalls[call.ID]; seen {
		if call.ID == s.activeCallID && (call.State != s.activeCallState || call.Number != s.activeCallNumber) {
			s.activeCallState = call.State
			s.activeCallNumber = call.Number
			s.config.Sink.UpdateCall(call)
		}
		return
	}
	s.seenCalls[call.ID] = struct{}{}
	if call.Direction == "incoming" && (call.State == "incoming" || call.State == "waiting") {
		s.activeCallID = call.ID
		s.activeCallState = call.State
		s.activeCallNumber = call.Number
		s.config.Sink.ShowCall(call)
	}
	// Calls that arrive already active (answered before the poller saw the
	// ringing state) are recorded but do not interrupt the user.
}

func (s *Service) applyCallEnd(call CallEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seenCalls[call.ID] = struct{}{}
	if call.ID == s.activeCallID {
		s.activeCallID = ""
		s.activeCallState = ""
		s.activeCallNumber = ""
		s.config.Sink.HideCall(call)
	}
	if call.Missed && call.Direction == "incoming" {
		s.config.Sink.ShowMissedCall(call)
	}
}

func (s *Service) applySMS(message SMSMessageEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := message.DedupKey()
	if _, seen := s.seenSMS[key]; seen {
		return
	}
	s.seenSMS[key] = struct{}{}
	s.config.Sink.ShowSMS(message)
}

// applyDeviceState counts offline signals; recovery resets the streak. The
// optional prompt carries the offline detail when the event itself was a
// device.offline publication.
func (s *Service) applyDeviceState(state device.State, offline *DeviceOfflineEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == device.StateReady {
		s.offlineCount = 0
		s.offlineShown = false
		return
	}
	if !isOfflineState(state) {
		return
	}
	s.offlineCount++
	if s.offlineCount >= OfflineErrorThreshold && !s.offlineShown && offline != nil {
		s.offlineShown = true
		s.config.Sink.ShowOffline(*offline)
	}
}

func isOfflineState(state device.State) bool {
	switch state {
	case device.StateAbsent, device.StateDisconnected, device.StateDegraded:
		return true
	}
	return false
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
			Body:       firstNonEmpty(request.Body, "DJOneHubNotifier debug message; code 482913"),
			Code:       firstNonEmpty(request.Code, "482913"),
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
	case DebugGPSSearching:
		return []runtime.Event{s.config.Events.Publish(EventGPSUpdated, GPSUpdateEvent{Enabled: true, LastChecked: now})}, nil
	case DebugGPSFix:
		return []runtime.Event{s.config.Events.Publish(EventGPSUpdated, GPSUpdateEvent{
			Enabled:     true,
			Fix:         &GPSFixEvent{UTC: "100000.000", Latitude: "31.2304", Longitude: "121.4737", HDOP: "1.1", Satellites: "12"},
			LastChecked: now,
		})}, nil
	case DebugGPSDisabled:
		return []runtime.Event{s.config.Events.Publish(EventGPSUpdated, GPSUpdateEvent{LastChecked: now})}, nil
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
	case CommandOpenDashboard, CommandToggleGPSPanel, CommandOpenGPSPanel, CommandCloseGPSPanel:
		// No parameters are expected.
	case CommandNotificationPermissionStatus:
		if !ValidNotificationPermissionState(command.Param("state")) {
			return fmt.Errorf("notification_permission_status requires a valid state")
		}
	default:
		return fmt.Errorf("unknown native command %q", command.Name)
	}
	return nil
}
