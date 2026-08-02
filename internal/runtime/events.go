package runtime

import (
	"sync"
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
	mu      sync.RWMutex
	seq     uint64
	subs    map[uint64]chan Event
	nextSub uint64
}

func NewEventBus() *EventBus { return &EventBus{subs: make(map[uint64]chan Event)} }

func (b *EventBus) Publish(eventType string, data any) Event {
	b.mu.Lock()
	b.seq++
	event := Event{ID: b.seq, Type: eventType, Version: 1, OccurredAt: time.Now().UTC(), Data: data}
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
			// A slow subscriber must not block device lifecycle ownership.
		}
	}
	b.mu.Unlock()
	return event
}

func (b *EventBus) Subscribe(buffer int) (uint64, <-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	b.nextSub++
	id := b.nextSub
	ch := make(chan Event, buffer)
	b.subs[id] = ch
	b.mu.Unlock()
	return id, ch, func() {
		b.mu.Lock()
		if current, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(current)
		}
		b.mu.Unlock()
	}
}

func (b *EventBus) LastID() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}
