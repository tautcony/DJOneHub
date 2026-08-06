package runtime

import (
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	ID         uint64    `json:"id"`
	Type       string    `json:"type"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	Data       any       `json:"data"`
}

type EventBus struct {
	mu       sync.RWMutex
	seq      uint64
	subs     map[uint64]chan Event
	subDrops map[uint64]*atomic.Uint64
	nextSub  uint64
	// accumulatedDrops counts every event dropped for a slow subscriber across
	// all subscriptions, past and present. It is monotonic: unsubscribe removes
	// the per-subscriber counter but never rewinds the cumulative total.
	accumulatedDrops atomic.Uint64
}

func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[uint64]chan Event), subDrops: make(map[uint64]*atomic.Uint64)}
}

// Publish broadcasts an event to every subscriber without ever blocking. A
// subscriber whose buffer is full has the event dropped and counted, so a slow
// consumer can stall nothing and silent loss stays diagnosable.
func (b *EventBus) Publish(eventType string, data any) Event {
	b.mu.Lock()
	b.seq++
	event := Event{ID: b.seq, Type: eventType, Version: 1, OccurredAt: time.Now().UTC(), Data: data}
	for id, ch := range b.subs {
		select {
		case ch <- event:
		default:
			// A slow subscriber must not block device lifecycle ownership;
			// count the drop instead of discarding it silently.
			b.accumulatedDrops.Add(1)
			if counter, ok := b.subDrops[id]; ok {
				counter.Add(1)
			}
		}
	}
	b.mu.Unlock()
	return event
}

// Subscription is the handle returned by SubscribeWithWatermark. The watermark
// is the bus sequence captured atomically with the subscription, so every event
// delivered afterwards has ID > watermark.
type Subscription struct {
	ID          uint64
	Events      <-chan Event
	Watermark   uint64
	DropCount   func() uint64
	Unsubscribe func()
}

// SubscribeWithWatermark subscribes and captures the current sequence under the
// bus lock, so a new subscriber can send a snapshot stamped with the watermark
// and then forward every event with ID > watermark without a gap.
func (b *EventBus) SubscribeWithWatermark(buffer int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	b.nextSub++
	id := b.nextSub
	ch := make(chan Event, buffer)
	counter := &atomic.Uint64{}
	b.subs[id] = ch
	b.subDrops[id] = counter
	watermark := b.seq
	b.mu.Unlock()
	var once sync.Once
	return Subscription{
		ID:        id,
		Events:    ch,
		Watermark: watermark,
		DropCount: func() uint64 { return counter.Load() },
		Unsubscribe: func() {
			once.Do(func() {
				b.mu.Lock()
				if current, ok := b.subs[id]; ok {
					delete(b.subs, id)
					delete(b.subDrops, id)
					close(current)
				}
				b.mu.Unlock()
			})
		},
	}
}

func (b *EventBus) Subscribe(buffer int) (uint64, <-chan Event, func()) {
	sub := b.SubscribeWithWatermark(buffer)
	return sub.ID, sub.Events, sub.Unsubscribe
}

func (b *EventBus) LastID() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// DropCounts is the read-only drop diagnostics snapshot exposed through the
// notification-debug response. Active entries are keyed by subscription ID and
// disappear on unsubscribe; Cumulative never decreases.
type DropCounts struct {
	Cumulative uint64            `json:"cumulative"`
	Active     map[uint64]uint64 `json:"active_subscribers"`
}

func (b *EventBus) DropCounts() DropCounts {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := DropCounts{Cumulative: b.accumulatedDrops.Load(), Active: make(map[uint64]uint64, len(b.subDrops))}
	for id, counter := range b.subDrops {
		out.Active[id] = counter.Load()
	}
	return out
}
