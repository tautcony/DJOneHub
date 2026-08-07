package extras

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/runtime"
)

type recordedCallEvent struct {
	Type string
	Data notification.CallEvent
}

// collectCallEvents runs fn and returns the first count call events
// published on the bus.
func collectCallEvents(t *testing.T, count int, fn func(*Service)) []recordedCallEvent {
	t.Helper()
	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil)
	_, events, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	fn(service)
	deadline := time.Now().Add(time.Second)
	var collected []recordedCallEvent
	for len(collected) < count {
		select {
		case event := <-events:
			switch event.Type {
			case notification.EventCallIncoming, notification.EventCallUpdated, notification.EventCallEnded, notification.EventCallMissed:
				if call, ok := event.Data.(notification.CallEvent); ok {
					collected = append(collected, recordedCallEvent{Type: event.Type, Data: call})
				}
			}
		case <-time.After(time.Until(deadline)):
			return collected
		}
	}
	return collected
}

func candidate(index int, direction, state, number string) callCandidate {
	return callCandidate{Index: index, Direction: direction, State: state, Number: number}
}

func TestApplyCallsPublishesIncomingThenEnded(t *testing.T) {
	collected := collectCallEvents(t, 2, func(service *Service) {
		now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
		// An empty first snapshot establishes the startup baseline: any call
		// seen afterwards is new.
		service.applyCalls(nil, now, "")
		service.applyCalls(nil, now.Add(time.Second), "")
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(3*time.Second), "")
		service.applyCalls(nil, now.Add(48*time.Second), "")
	})
	if len(collected) != 2 {
		t.Fatalf("expected 2 events, got %d", len(collected))
	}
	if collected[0].Type != notification.EventCallIncoming {
		t.Errorf("first event = %s, want call.incoming", collected[0].Type)
	}
	if collected[0].Data.State != "incoming" || collected[0].Data.Number != "18900007376" {
		t.Errorf("incoming = %+v", collected[0].Data)
	}
	if collected[1].Type != notification.EventCallMissed {
		t.Errorf("second event = %s, want call.missed", collected[1].Type)
	}
	if !collected[1].Data.Missed || collected[1].Data.EndedAt == nil {
		t.Errorf("missed = %+v", collected[1].Data)
	}
}

func TestApplyCallsPublishesUpdatedOnlyOnChange(t *testing.T) {
	collected := collectCallEvents(t, 2, func(service *Service) {
		now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
		service.applyCalls(nil, now, "")
		service.applyCalls(nil, now.Add(time.Second), "")
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(3*time.Second), "")
		// Same state and number: no new event.
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(6*time.Second), "")
		// State change: call.updated.
		service.applyCalls([]callCandidate{candidate(1, "incoming", "active", "18900007376")}, now.Add(9*time.Second), "")
	})
	if len(collected) != 2 {
		t.Fatalf("expected 2 events (incoming + updated), got %d: %+v", len(collected), collected)
	}
	if collected[0].Type != notification.EventCallIncoming || collected[1].Type != notification.EventCallUpdated {
		t.Errorf("events = %+v", collected)
	}
	if collected[1].Data.State != "active" {
		t.Errorf("updated = %+v", collected[1].Data)
	}
}

func TestApplyCallsPublishesEndedNotMissedForAnsweredCall(t *testing.T) {
	collected := collectCallEvents(t, 3, func(service *Service) {
		now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
		service.applyCalls(nil, now, "")
		service.applyCalls(nil, now.Add(time.Second), "")
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(3*time.Second), "")
		service.applyCalls([]callCandidate{candidate(1, "incoming", "active", "18900007376")}, now.Add(6*time.Second), "")
		service.applyCalls(nil, now.Add(63*time.Second), "")
	})
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(collected), collected)
	}
	if collected[2].Type != notification.EventCallEnded || collected[2].Data.Missed {
		t.Errorf("answered call must end as call.ended without missed flag: %+v", collected[2])
	}
}

func TestApplyCallsArchivesReplacedActiveCall(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	collected := collectCallEvents(t, 3, func(service *Service) {
		service.applyCalls(nil, now, "")
		service.applyCalls(nil, now.Add(time.Second), "")
		incomingAt := now.Add(3 * time.Second)
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, incomingAt, "")
		// A second call replaces the first; the first is archived and ends.
		service.applyCalls([]callCandidate{candidate(2, "incoming", "waiting", "18800000000")}, incomingAt.Add(10*time.Second), "")
	})
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(collected), collected)
	}
	if collected[0].Type != notification.EventCallIncoming || collected[0].Data.ID != fmt.Sprintf("%d-1", now.Add(3*time.Second).UnixMilli()) {
		t.Errorf("first event must be the original incoming: %+v", collected[0])
	}
	if collected[1].Type != notification.EventCallMissed {
		t.Errorf("replacement must archive the first call as missed: %+v", collected[1])
	}
	if collected[2].Type != notification.EventCallIncoming || collected[2].Data.Number != "18800000000" {
		t.Errorf("second call must ring as incoming: %+v", collected[2])
	}
}

// TestApplyCallsBaselineSnapshotIsSilent covers the startup leftover: the
// first snapshot sees a call already ringing before the app started. It must
// be tracked (and archived as history) without any incoming or missed prompt.
func TestApplyCallsBaselineSnapshotIsSilent(t *testing.T) {
	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil)
	_, events, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	// First successful snapshot is the baseline: a leftover incoming call is
	// established silently.
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now, "")
	// The leftover call disappears without being answered: still no events.
	service.applyCalls(nil, now.Add(45*time.Second), "")

	select {
	case event := <-events:
		t.Fatalf("unexpected event %s %+v for startup leftover call", event.Type, event.Data)
	case <-time.After(200 * time.Millisecond):
	}
	status := service.Calls(context.Background())
	if status.Active != nil {
		t.Errorf("active = %+v, want nil", status.Active)
	}
	if len(status.History) != 1 || !status.History[0].Missed || status.History[0].Number != "18900007376" {
		t.Errorf("history = %+v, want the leftover call archived as missed", status.History)
	}
}

// TestApplyCallsLateBaselineSnapshotIsSilent covers a modem that returns an
// empty CLCC response first and exposes its stale call on the next poll.
func TestApplyCallsLateBaselineSnapshotIsSilent(t *testing.T) {
	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil)
	_, events, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	service.applyCalls(nil, now, "")
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(3*time.Second), "")
	service.applyCalls(nil, now.Add(6*time.Second), "")

	select {
	case event := <-events:
		t.Fatalf("unexpected event %s %+v for late startup leftover", event.Type, event.Data)
	case <-time.After(200 * time.Millisecond):
	}
	status := service.Calls(context.Background())
	if len(status.History) != 1 || !status.History[0].Missed {
		t.Errorf("history = %+v, want the late leftover archived as missed", status.History)
	}
}

// TestApplyCallsBaselineCallLifecycleIsSilent covers a leftover call that is
// answered and then hangs up after the baseline: no updated/ended/missed
// prompt may be published for a call the user was never notified about.
func TestApplyCallsBaselineCallLifecycleIsSilent(t *testing.T) {
	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil)
	_, events, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now, "")
	// Answered: state change must not publish call.updated.
	service.applyCalls([]callCandidate{candidate(1, "incoming", "active", "18900007376")}, now.Add(3*time.Second), "")
	// Hang up: must not publish call.ended or call.missed.
	service.applyCalls(nil, now.Add(60*time.Second), "")

	select {
	case event := <-events:
		t.Fatalf("unexpected event %s %+v for leftover call lifecycle", event.Type, event.Data)
	case <-time.After(200 * time.Millisecond):
	}
	status := service.Calls(context.Background())
	if len(status.History) != 1 || status.History[0].Missed {
		t.Errorf("answered leftover call must be archived as ended, got %+v", status.History)
	}
}

// TestApplyCallsBaselineCallReplacementAnnouncesNewCall covers a new call
// arriving while a leftover call is still present: only the new call is
// announced; the replaced leftover ends silently.
func TestApplyCallsBaselineCallReplacementAnnouncesNewCall(t *testing.T) {
	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil)
	_, events, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now, "")
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(time.Second), "")
	service.applyCalls([]callCandidate{candidate(2, "incoming", "waiting", "18800000000")}, now.Add(10*time.Second), "")

	var collected []recordedCallEvent
	deadline := time.Now().Add(time.Second)
	for len(collected) < 1 {
		select {
		case event := <-events:
			if call, ok := event.Data.(notification.CallEvent); ok {
				collected = append(collected, recordedCallEvent{Type: event.Type, Data: call})
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("expected the new call to be announced, got %d events", len(collected))
		}
	}
	if len(collected) != 1 {
		t.Fatalf("expected exactly 1 event (the new call), got %d: %+v", len(collected), collected)
	}
	if collected[0].Type != notification.EventCallIncoming || collected[0].Data.Number != "18800000000" {
		t.Errorf("event = %+v, want call.incoming for the new number", collected[0])
	}
	status := service.Calls(context.Background())
	if len(status.History) != 1 || !status.History[0].Missed || status.History[0].Number != "18900007376" {
		t.Errorf("history = %+v, want the replaced leftover archived as missed", status.History)
	}
	if status.Active == nil || status.Active.Number != "18800000000" {
		t.Errorf("active = %+v, want the new call", status.Active)
	}
}
