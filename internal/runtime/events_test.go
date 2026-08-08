package runtime

import (
	"testing"
)

func TestPublishNeverBlocksSlowSubscriberAndCountsDrops(t *testing.T) {
	bus := NewEventBus()
	_, events, unsubscribe := bus.Subscribe(1)
	defer unsubscribe()
	// The subscriber never drains; the second and third publishes overflow
	// the single-slot buffer and must be dropped, not block.
	bus.Publish("a", nil)
	bus.Publish("b", nil)
	bus.Publish("c", nil)

	drops := bus.DropCounts()
	if drops.Cumulative != 2 {
		t.Fatalf("cumulative drops = %d, want 2", drops.Cumulative)
	}
	if len(drops.Active) != 1 {
		t.Fatalf("active subscriber entries = %d, want 1", len(drops.Active))
	}
	for _, count := range drops.Active {
		if count != 2 {
			t.Fatalf("active subscriber drop = %d, want 2", count)
		}
	}
	// The buffered event is still delivered intact.
	event := <-events
	if event.Type != "a" {
		t.Fatalf("event type = %q", event.Type)
	}
	// The bus keeps sequencing: the dropped events consumed IDs.
	if bus.LastID() != 3 {
		t.Fatalf("LastID = %d, want 3", bus.LastID())
	}
}

func TestUnsubscribeRemovesActiveSubscriberStateOnly(t *testing.T) {
	bus := NewEventBus()
	id, _, unsubscribe := bus.Subscribe(1)
	bus.Publish("a", nil)
	bus.Publish("b", nil) // dropped

	before := bus.DropCounts()
	if before.Cumulative != 1 {
		t.Fatalf("cumulative = %d, want 1", before.Cumulative)
	}
	if _, ok := before.Active[id]; !ok {
		t.Fatalf("subscription %d missing from active diagnostics", id)
	}

	unsubscribe()
	after := bus.DropCounts()
	// The cumulative count is monotonic; the per-subscriber entry is gone.
	if after.Cumulative != 1 {
		t.Fatalf("cumulative after unsubscribe = %d, want 1", after.Cumulative)
	}
	if len(after.Active) != 0 {
		t.Fatalf("active subscribers after unsubscribe = %v, want empty", after.Active)
	}
}

func TestSubscribeWithWatermarkCapturesSequenceUnderLock(t *testing.T) {
	bus := NewEventBus()
	bus.Publish("before", nil) // seq 1

	sub := bus.SubscribeWithWatermark(8)
	defer sub.Unsubscribe()
	if sub.Watermark != 1 {
		t.Fatalf("watermark = %d, want 1", sub.Watermark)
	}

	// Events published after subscribing are queued with ID > watermark.
	bus.Publish("during", nil) // seq 2
	bus.Publish("after", nil)  // seq 3
	for i, want := range []uint64{2, 3} {
		event := <-sub.Events
		if event.ID != want {
			t.Fatalf("event %d id = %d, want %d", i, event.ID, want)
		}
	}
	if sub.DropCount() != 0 {
		t.Fatalf("drop count = %d, want 0", sub.DropCount())
	}
	// Unsubscribe is idempotent and closes the channel once.
	sub.Unsubscribe()
	if sub.DropCount() != 0 {
		t.Fatalf("drop count after unsubscribe = %d, want 0", sub.DropCount())
	}
}

func TestSubscribeWithWatermarkCountsOverflow(t *testing.T) {
	bus := NewEventBus()
	sub := bus.SubscribeWithWatermark(1)
	defer sub.Unsubscribe()
	bus.Publish("a", nil) // fills the buffer
	bus.Publish("b", nil) // dropped
	if sub.DropCount() != 1 {
		t.Fatalf("drop count = %d, want 1", sub.DropCount())
	}
	if drops := bus.DropCounts(); drops.Cumulative != 1 {
		t.Fatalf("cumulative = %d, want 1", drops.Cumulative)
	}
}

func TestDiagnosticsNamesSubscribersAndOmitsPayload(t *testing.T) {
	bus := NewEventBus()
	_, _, unsubscribe := bus.SubscribeNamed("test-consumer", 1)
	defer unsubscribe()
	bus.Publish("sms.received", map[string]any{"body": "must-not-leak"})
	bus.Publish("sms.received", map[string]any{"body": "also-secret"})

	diagnostics := bus.Diagnostics()
	if diagnostics.Published != 2 || diagnostics.CumulativeDrops != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(diagnostics.Subscribers) != 1 || diagnostics.Subscribers[0].Name != "test-consumer" {
		t.Fatalf("subscribers = %#v", diagnostics.Subscribers)
	}
	if diagnostics.Subscribers[0].Queued != 1 || diagnostics.Subscribers[0].Dropped != 1 {
		t.Fatalf("subscriber pressure = %#v", diagnostics.Subscribers[0])
	}
	if len(diagnostics.Recent) != 2 || diagnostics.Recent[0].Type != "sms.received" {
		t.Fatalf("recent = %#v", diagnostics.Recent)
	}
}

func TestMessageTraceRecordsNamedDeliveryWithoutPayload(t *testing.T) {
	bus := NewEventBus()
	_, _, unsubscribe := bus.SubscribeNamed("notification-policy", 1)
	defer unsubscribe()
	event := bus.Publish("sms.received", map[string]any{"body": "private"})

	trace, ok := bus.MessageTrace(event.ID)
	if !ok {
		t.Fatal("message trace missing")
	}
	if trace.Type != "sms.received" || trace.Status != "success" {
		t.Fatalf("trace = %#v", trace)
	}
	wantNodes := []string{"sms-poller", "domain-events", "notification-policy"}
	if len(trace.Hops) != len(wantNodes) {
		t.Fatalf("hops = %#v", trace.Hops)
	}
	for i, want := range wantNodes {
		if trace.Hops[i].NodeID != want {
			t.Fatalf("hop %d node = %q, want %q", i, trace.Hops[i].NodeID, want)
		}
		if trace.Hops[i].Detail == "private" {
			t.Fatal("trace retained event payload")
		}
	}
}

func TestMessageTraceStreamingIsNonBlockingAndReportsUpdates(t *testing.T) {
	bus := NewEventBus()
	sub := bus.SubscribeMessageTraces(1)
	defer sub.Unsubscribe()
	event := bus.Publish("operation.changed", nil)
	initial := <-sub.Updates
	if initial.ID != event.ID {
		t.Fatalf("initial trace id = %d, want %d", initial.ID, event.ID)
	}
	bus.RecordTraceHop(event.ID, "worker", "handle", "success", "")
	updated := <-sub.Updates
	if len(updated.Hops) != len(initial.Hops)+1 || updated.Hops[len(updated.Hops)-1].NodeID != "worker" {
		t.Fatalf("updated trace = %#v", updated)
	}
}
