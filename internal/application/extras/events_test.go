package extras

import (
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
	service := NewService(nil, operation.NewManager(bus), nil, "")
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
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now)
		service.applyCalls(nil, now.Add(45*time.Second))
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
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now)
		// Same state and number: no new event.
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now.Add(3*time.Second))
		// State change: call.updated.
		service.applyCalls([]callCandidate{candidate(1, "incoming", "active", "18900007376")}, now.Add(6*time.Second))
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
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now)
		service.applyCalls([]callCandidate{candidate(1, "incoming", "active", "18900007376")}, now.Add(3*time.Second))
		service.applyCalls(nil, now.Add(60*time.Second))
	})
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(collected), collected)
	}
	if collected[2].Type != notification.EventCallEnded || collected[2].Data.Missed {
		t.Errorf("answered call must end as call.ended without missed flag: %+v", collected[2])
	}
}

func TestApplyCallsArchivesReplacedActiveCall(t *testing.T) {
	collected := collectCallEvents(t, 3, func(service *Service) {
		now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
		service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "18900007376")}, now)
		// A second call replaces the first; the first is archived and ends.
		service.applyCalls([]callCandidate{candidate(2, "incoming", "waiting", "18800000000")}, now.Add(10*time.Second))
	})
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(collected), collected)
	}
	if collected[0].Type != notification.EventCallIncoming || collected[0].Data.ID != "1785664800000-1" {
		t.Errorf("first event must be the original incoming: %+v", collected[0])
	}
	if collected[1].Type != notification.EventCallMissed {
		t.Errorf("replacement must archive the first call as missed: %+v", collected[1])
	}
	if collected[2].Type != notification.EventCallIncoming || collected[2].Data.Number != "18800000000" {
		t.Errorf("second call must ring as incoming: %+v", collected[2])
	}
}
