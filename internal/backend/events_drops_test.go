package backend

import (
	"context"
	"testing"
	"time"

	qmimanager "github.com/iniwex5/quectel-qmi-go/pkg/manager"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/modem"
)

// eventSourceStub exposes OnEvent so QMIBackend.Events can register the
// dispatch callback; the embedded stub implements the rest of QMISource.
type eventSourceStub struct {
	*qmiBackendSendSourceStub
	handler qmimanager.EventHandler
}

func (s *eventSourceStub) OnEvent(handler qmimanager.EventHandler) { s.handler = handler }

// TestQMIBackendDropsEventsForSlowSubscriberWithoutBlocking verifies the
// QMI event channel never blocks the dispatcher and counts drops for a
// subscriber that does not drain. The AT backend uses the identical
// select-default send shape (its RDY signal comes from the modem transport,
// which cannot be driven in a unit test), so this covers both paths.
func TestQMIBackendDropsEventsForSlowSubscriberWithoutBlocking(t *testing.T) {
	source := &eventSourceStub{qmiBackendSendSourceStub: &qmiBackendSendSourceStub{}}
	q, err := NewQMIBackend("test", source)
	if err != nil {
		t.Fatalf("NewQMIBackend: %v", err)
	}
	defer q.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := q.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if source.handler == nil {
		t.Fatal("OnEvent handler was not registered")
	}
	if q.EventDrops() != 0 {
		t.Fatalf("initial EventDrops = %d, want 0", q.EventDrops())
	}

	// The subscriber never drains: after the 16-slot buffer fills, further
	// dispatches must be dropped (and counted), not block the dispatcher.
	for i := 0; i < 64; i++ {
		source.handler(qmimanager.Event{Type: qmimanager.EventDisconnected, Reason: "test"})
	}
	// All 64 events were dispatched without blocking; the first 16 are
	// buffered, the rest dropped.
	if got := q.EventDrops(); got != 48 {
		t.Fatalf("EventDrops = %d, want 48", got)
	}
	count := 0
	for {
		select {
		case <-events:
			count++
			if count == 16 {
				goto drained
			}
		case <-time.After(time.Second):
			t.Fatalf("buffered events not delivered, got %d", count)
		}
	}
drained:
	select {
	case event := <-events:
		t.Fatalf("unexpected extra event %+v", event)
	default:
	}
}

// TestATBackendEventsLifecycle verifies the AT backend exposes a working
// event channel and a zero drop counter, and that Close unblocks the event
// goroutine so it cannot leak after shutdown.
func TestATBackendEventsLifecycle(t *testing.T) {
	m, err := modem.New(config.DeviceConfig{ID: "dev-test", DeviceBackend: "qmi"})
	if err != nil {
		t.Fatalf("modem.New: %v", err)
	}
	a := NewATBackend(m)
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := a.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if a.EventDrops() != 0 {
		t.Fatalf("initial EventDrops = %d, want 0", a.EventDrops())
	}
	// Close signals the event goroutine to exit; the channel closes and the
	// goroutine joins, so the adapter never leaks on shutdown.
	_ = a.Close()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("events channel must close after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("event goroutine did not exit after Close")
	}
}
