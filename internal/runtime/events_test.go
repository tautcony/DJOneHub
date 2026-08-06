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
