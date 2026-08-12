package runtime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const recentTraceLimit = 200

// TraceHop is one payload-free step in an event's in-process journey.
type TraceHop struct {
	NodeID     string    `json:"node_id"`
	FromNodeID string    `json:"from_node_id,omitempty"`
	Action     string    `json:"action"`
	State      string    `json:"state"`
	At         time.Time `json:"at"`
	Detail     string    `json:"detail,omitempty"`
}

// MessageTrace contains only allowlisted diagnostic fields, never the original
// event payload. It is kept in a bounded in-memory ring.
type MessageTrace struct {
	ID        uint64         `json:"id"`
	Type      string         `json:"type"`
	StartedAt time.Time      `json:"started_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Status    string         `json:"status"`
	Fields    map[string]any `json:"fields,omitempty"`
	Hops      []TraceHop     `json:"hops"`
}

type TraceSubscription struct {
	Updates     <-chan MessageTrace
	Unsubscribe func()
}

type TraceStore struct {
	mu      sync.RWMutex
	traces  map[uint64]*MessageTrace
	order   []uint64
	subs    map[uint64]chan MessageTrace
	nextSub uint64
}

func newTraceStore() *TraceStore {
	return &TraceStore{traces: make(map[uint64]*MessageTrace), subs: make(map[uint64]chan MessageTrace)}
}

func traceSource(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "sms."):
		return "sms-poller"
	case strings.HasPrefix(eventType, "call."):
		return "call-poller"
	case strings.HasPrefix(eventType, "network."):
		return "network-poller"
	case strings.HasPrefix(eventType, "traffic."):
		return "traffic-poller"
	case strings.HasPrefix(eventType, "operation."):
		return "operations"
	case strings.HasPrefix(eventType, "vowifi."):
		return "vowifi-runtime"
	case strings.HasPrefix(eventType, "backend."), strings.HasPrefix(eventType, "sim."):
		return "backend-events"
	case strings.HasPrefix(eventType, "device."):
		return "runtime-scan"
	default:
		return "application"
	}
}

func (s *TraceStore) start(event Event, deliveries []TraceHop, fields map[string]any) {
	now := event.OccurredAt
	hops := []TraceHop{
		{NodeID: traceSource(event.Type), Action: "publish", State: "success", At: now},
		{NodeID: "domain-events", FromNodeID: traceSource(event.Type), Action: "fan-out", State: "success", At: now},
	}
	hops = append(hops, deliveries...)
	trace := &MessageTrace{
		ID: event.ID, Type: event.Type, StartedAt: now, UpdatedAt: now,
		Status: "success", Fields: cloneTraceFields(fields), Hops: hops,
	}
	for _, hop := range deliveries {
		if hop.State == "dropped" || hop.State == "failed" {
			trace.Status = hop.State
			break
		}
	}
	s.mu.Lock()
	s.traces[event.ID] = trace
	s.order = append(s.order, event.ID)
	if len(s.order) > recentTraceLimit {
		oldest := s.order[0]
		s.order = append([]uint64(nil), s.order[1:]...)
		delete(s.traces, oldest)
	}
	snapshot, subscribers := cloneTrace(*trace), s.subscribersLocked()
	s.mu.Unlock()
	publishTrace(subscribers, snapshot)
}

func (s *TraceStore) Record(id uint64, nodeID, action, state, detail string) {
	if id == 0 || nodeID == "" {
		return
	}
	s.recordHop(id, TraceHop{NodeID: nodeID, FromNodeID: traceParent(nodeID), Action: action, State: state, At: time.Now().UTC(), Detail: detail})
}

func traceParent(nodeID string) string {
	switch nodeID {
	case "notification-policy", "vowifi-runtime", "browser-websocket":
		return "domain-events"
	case "notification-queue":
		return "notification-policy"
	case "native-ui", "telegram", "feishu", "webhook", "bark", "email", "pushplus":
		return "notification-queue"
	default:
		return "domain-events"
	}
}

func (s *TraceStore) recordHop(id uint64, hop TraceHop) {
	s.mu.Lock()
	trace := s.traces[id]
	if trace == nil {
		s.mu.Unlock()
		return
	}
	trace.Hops = append(trace.Hops, hop)
	sort.SliceStable(trace.Hops, func(i, j int) bool { return trace.Hops[i].At.Before(trace.Hops[j].At) })
	if hop.At.After(trace.UpdatedAt) {
		trace.UpdatedAt = hop.At
	}
	if hop.State == "failed" || hop.State == "dropped" {
		trace.Status = hop.State
	}
	snapshot, subscribers := cloneTrace(*trace), s.subscribersLocked()
	s.mu.Unlock()
	publishTrace(subscribers, snapshot)
}

func (s *TraceStore) Recent() []MessageTrace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MessageTrace, 0, len(s.order))
	for _, id := range s.order {
		if trace := s.traces[id]; trace != nil {
			out = append(out, cloneTrace(*trace))
		}
	}
	return out
}

func (s *TraceStore) Get(id uint64) (MessageTrace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trace := s.traces[id]
	if trace == nil {
		return MessageTrace{}, false
	}
	return cloneTrace(*trace), true
}

func (s *TraceStore) Subscribe(buffer int) TraceSubscription {
	if buffer < 1 {
		buffer = 1
	}
	s.mu.Lock()
	s.nextSub++
	id := s.nextSub
	ch := make(chan MessageTrace, buffer)
	s.subs[id] = ch
	s.mu.Unlock()
	var once sync.Once
	return TraceSubscription{Updates: ch, Unsubscribe: func() {
		once.Do(func() {
			s.mu.Lock()
			if s.subs[id] != nil {
				delete(s.subs, id)
			}
			s.mu.Unlock()
		})
	}}
}

func (s *TraceStore) subscribersLocked() []chan MessageTrace {
	out := make([]chan MessageTrace, 0, len(s.subs))
	for _, ch := range s.subs {
		out = append(out, ch)
	}
	return out
}

func cloneTrace(trace MessageTrace) MessageTrace {
	trace.Fields = cloneTraceFields(trace.Fields)
	trace.Hops = append([]TraceHop(nil), trace.Hops...)
	return trace
}

func cloneTraceFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}

// traceFields projects event payloads to a small diagnostic allowlist. Message
// bodies, phone numbers, subscriber identities and free-form error text never
// enter the trace store.
func traceFields(eventType string, data any) map[string]any {
	allowed := traceFieldAllowlist(eventType)
	if len(allowed) == 0 || data == nil {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil
	}
	out := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if value, ok := values[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func traceFieldAllowlist(eventType string) []string {
	switch {
	case eventType == "device.status.changed":
		return []string{"state", "generation", "backend"}
	case eventType == "device.offline":
		return []string{"state"}
	case eventType == "network.updated":
		return []string{"mode", "network_mode", "registered", "sim_inserted", "sim_known", "radio_band", "signal_dbm", "signal_rsrp", "signal_rsrq", "signal_snr"}
	case eventType == "network.traffic.updated" || eventType == "traffic.updated":
		return []string{"rx_bytes", "tx_bytes", "daily_rx_bytes", "daily_tx_bytes", "daily_available", "sampled_at"}
	case eventType == "sms.received":
		return []string{"index", "received_at", "recorded_at"}
	case eventType == "sms.updated":
		return []string{"count", "cleared", "operation"}
	case strings.HasPrefix(eventType, "call."):
		return []string{"id", "direction", "state", "started_at", "ended_at", "connected_at", "missed"}
	case strings.HasPrefix(eventType, "operation."):
		return []string{"operation_id", "type", "state", "progress", "started_at", "finished_at"}
	case eventType == "at.updated":
		return []string{"command_class", "completed"}
	case eventType == "esim.updated":
		return []string{"operation"}
	case eventType == "vowifi.updated" || eventType == "vowifi.state.changed":
		return []string{"state", "operation"}
	case eventType == "device.rebooted":
		return []string{"accepted"}
	default:
		return nil
	}
}

func publishTrace(subscribers []chan MessageTrace, trace MessageTrace) {
	for _, ch := range subscribers {
		select {
		case ch <- trace:
		default:
			// Trace streaming is best effort and must never pressure EventBus.
		}
	}
}

func subscriberNode(name string, id uint64) string {
	switch name {
	case "notification-policy", "vowifi-runtime":
		return name
	case "websocket-client":
		return "browser-websocket"
	case "anonymous":
		return fmt.Sprintf("subscriber-%d", id)
	default:
		return name
	}
}
