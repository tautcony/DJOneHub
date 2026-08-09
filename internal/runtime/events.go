package runtime

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const recentEventLimit = 40

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
	subNames map[uint64]string
	subSince map[uint64]time.Time
	types    map[string]EventTypeDiagnostics
	recent   []EventTrace
	nextSub  uint64
	// accumulatedDrops counts every event dropped for a slow subscriber across
	// all subscriptions, past and present. It is monotonic: unsubscribe removes
	// the per-subscriber counter but never rewinds the cumulative total.
	accumulatedDrops atomic.Uint64
	traces           *TraceStore
}

func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[uint64]chan Event), subDrops: make(map[uint64]*atomic.Uint64),
		subNames: make(map[uint64]string), subSince: make(map[uint64]time.Time),
		types:  make(map[string]EventTypeDiagnostics),
		traces: newTraceStore(),
	}
}

// Publish broadcasts an event to every subscriber without ever blocking. A
// subscriber whose buffer is full has the event dropped and counted, so a slow
// consumer can stall nothing and silent loss stays diagnosable.
func (b *EventBus) Publish(eventType string, data any) Event {
	return b.publish(eventType, data, traceFields(eventType, data))
}

func (b *EventBus) publish(eventType string, data any, fields map[string]any) Event {
	b.mu.Lock()
	b.seq++
	event := Event{ID: b.seq, Type: eventType, Version: 1, OccurredAt: time.Now().UTC(), Data: data}
	// Create the safely projected trace before any subscriber can receive the
	// event. A fast consumer can therefore append downstream hops immediately.
	b.traces.start(event, nil, fields)
	delivered := 0
	dropped := 0
	deliveries := make([]TraceHop, 0, len(b.subs))
	for id, ch := range b.subs {
		hop := TraceHop{NodeID: subscriberNode(b.subNames[id], id), FromNodeID: "domain-events", Action: "enqueue", At: event.OccurredAt}
		select {
		case ch <- event:
			delivered++
			hop.State = "success"
		default:
			// A slow subscriber must not block device lifecycle ownership;
			// count the drop instead of discarding it silently.
			b.accumulatedDrops.Add(1)
			dropped++
			hop.State = "dropped"
			hop.Detail = "subscriber queue full"
			if counter, ok := b.subDrops[id]; ok {
				counter.Add(1)
			}
		}
		deliveries = append(deliveries, hop)
	}
	stats := b.types[eventType]
	stats.Type = eventType
	stats.Count++
	stats.LastID = event.ID
	stats.LastOccurredAt = event.OccurredAt
	b.types[eventType] = stats
	b.recent = append(b.recent, EventTrace{
		ID: event.ID, Type: event.Type, OccurredAt: event.OccurredAt,
		Subscribers: len(b.subs), Delivered: delivered, Dropped: dropped,
	})
	if len(b.recent) > recentEventLimit {
		b.recent = append([]EventTrace(nil), b.recent[len(b.recent)-recentEventLimit:]...)
	}
	b.mu.Unlock()
	for _, hop := range deliveries {
		b.traces.recordHop(event.ID, hop)
	}
	return event
}

func (b *EventBus) publishDerived(eventType string, data any, sourceEvent string) Event {
	fields := traceFields(eventType, data)
	if fields == nil {
		fields = make(map[string]any, 1)
	}
	fields["derived_from"] = sourceEvent
	return b.publish(eventType, data, fields)
}

func (b *EventBus) RecordTraceHop(eventID uint64, nodeID, action, state, detail string) {
	b.traces.Record(eventID, nodeID, action, state, detail)
}

func (b *EventBus) RecentMessageTraces() []MessageTrace { return b.traces.Recent() }

func (b *EventBus) MessageTrace(id uint64) (MessageTrace, bool) { return b.traces.Get(id) }

func (b *EventBus) SubscribeMessageTraces(buffer int) TraceSubscription {
	return b.traces.Subscribe(buffer)
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
	return b.SubscribeWithWatermarkNamed("anonymous", buffer)
}

func (b *EventBus) SubscribeWithWatermarkNamed(name string, buffer int) Subscription {
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
	if name == "" {
		name = "anonymous"
	}
	b.subNames[id] = name
	b.subSince[id] = time.Now().UTC()
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
					delete(b.subNames, id)
					delete(b.subSince, id)
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

func (b *EventBus) SubscribeNamed(name string, buffer int) (uint64, <-chan Event, func()) {
	sub := b.SubscribeWithWatermarkNamed(name, buffer)
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

type EventTypeDiagnostics struct {
	Type           string    `json:"type"`
	Count          uint64    `json:"count"`
	LastID         uint64    `json:"last_id"`
	LastOccurredAt time.Time `json:"last_occurred_at"`
}

type EventTrace struct {
	ID          uint64    `json:"id"`
	Type        string    `json:"type"`
	OccurredAt  time.Time `json:"occurred_at"`
	Subscribers int       `json:"subscribers"`
	Delivered   int       `json:"delivered"`
	Dropped     int       `json:"dropped"`
}

type SubscriberDiagnostics struct {
	ID       uint64    `json:"id"`
	Name     string    `json:"name"`
	Queued   int       `json:"queued"`
	Capacity int       `json:"capacity"`
	Dropped  uint64    `json:"dropped"`
	Since    time.Time `json:"since"`
}

type EventBusDiagnostics struct {
	Published       uint64                  `json:"published"`
	CumulativeDrops uint64                  `json:"cumulative_drops"`
	Subscribers     []SubscriberDiagnostics `json:"subscribers"`
	EventTypes      []EventTypeDiagnostics  `json:"event_types"`
	Recent          []EventTrace            `json:"recent"`
}

func (b *EventBus) Diagnostics() EventBusDiagnostics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := EventBusDiagnostics{
		Published: b.seq, CumulativeDrops: b.accumulatedDrops.Load(),
		Subscribers: make([]SubscriberDiagnostics, 0, len(b.subs)),
		EventTypes:  make([]EventTypeDiagnostics, 0, len(b.types)),
		Recent:      append([]EventTrace(nil), b.recent...),
	}
	for id, ch := range b.subs {
		out.Subscribers = append(out.Subscribers, SubscriberDiagnostics{
			ID: id, Name: b.subNames[id], Queued: len(ch), Capacity: cap(ch),
			Dropped: b.subDrops[id].Load(), Since: b.subSince[id],
		})
	}
	for _, stats := range b.types {
		out.EventTypes = append(out.EventTypes, stats)
	}
	sort.Slice(out.Subscribers, func(i, j int) bool { return out.Subscribers[i].ID < out.Subscribers[j].ID })
	sort.Slice(out.EventTypes, func(i, j int) bool {
		if out.EventTypes[i].Count == out.EventTypes[j].Count {
			return out.EventTypes[i].Type < out.EventTypes[j].Type
		}
		return out.EventTypes[i].Count > out.EventTypes[j].Count
	})
	return out
}
