package httpapi

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"
	gort "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/snapshot"
	"github.com/iniwex5/vohive/internal/notify"
	"github.com/iniwex5/vohive/internal/platform/native"
	appRuntime "github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/pkg/logger"
)

type workerDiagnostics struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	State         string    `json:"state"`
	Detail        string    `json:"detail"`
	IntervalMS    int64     `json:"interval_ms,omitempty"`
	EventSource   bool      `json:"event_source,omitempty"`
	EventTypes    []string  `json:"event_types,omitempty"`
	QueueDepth    int       `json:"queue_depth,omitempty"`
	QueueCapacity int       `json:"queue_capacity,omitempty"`
	Dropped       uint64    `json:"dropped,omitempty"`
	LastActivity  time.Time `json:"last_activity,omitempty"`
}

type channelDiagnostics struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	Detail        string `json:"detail"`
	Published     uint64 `json:"published,omitempty"`
	Dropped       uint64 `json:"dropped,omitempty"`
	Subscribers   int    `json:"subscribers,omitempty"`
	QueueDepth    int    `json:"queue_depth,omitempty"`
	QueueCapacity int    `json:"queue_capacity,omitempty"`
}

type flowDiagnostics struct {
	ID         string   `json:"id"`
	From       string   `json:"from"`
	Via        string   `json:"via"`
	To         []string `json:"to"`
	EventTypes []string `json:"event_types"`
}

type runtimeDiagnosticsResponse struct {
	GeneratedAt      time.Time                      `json:"generated_at"`
	UptimeSeconds    int64                          `json:"uptime_seconds"`
	Goroutines       int                            `json:"goroutines"`
	Workers          []workerDiagnostics            `json:"workers"`
	Channels         []channelDiagnostics           `json:"channels"`
	EventBus         appRuntime.EventBusDiagnostics `json:"event_bus"`
	Flows            []flowDiagnostics              `json:"flows"`
	Topology         topologyDiagnostics            `json:"topology"`
	Traces           []appRuntime.MessageTrace      `json:"traces"`
	ChannelRecovery  []notify.ChannelRecovery       `json:"channel_recovery"`
	RoutePerformance []RoutePerformanceSummary      `json:"route_performance"`
	Snapshots        []snapshot.Summary             `json:"snapshots"`
}

type topologyNodeDiagnostics struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type topologyEdgeDiagnostics struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	EventTypes []string `json:"event_types,omitempty"`
}

type topologyDiagnostics struct {
	Nodes []topologyNodeDiagnostics `json:"nodes"`
	Edges []topologyEdgeDiagnostics `json:"edges"`
}

func workerState(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

func (s *Server) runtimeDiagnostics(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	var runtimeStats appRuntime.Diagnostics
	var bus appRuntime.EventBusDiagnostics
	if s.config.Runtime != nil {
		runtimeStats = s.config.Runtime.Diagnostics()
		bus = s.config.Runtime.Events().Diagnostics()
	}
	operationStats := operation.Diagnostics{ByState: map[operation.State]int{}}
	if s.config.Operations != nil {
		operationStats = s.config.Operations.Diagnostics()
	}
	notificationStats := notification.Diagnostics{}
	if s.config.Notification != nil {
		notificationStats = s.config.Notification.Diagnostics()
	}
	channelStats := notify.Diagnostics{}
	if s.config.NotificationChannelsDiagnostics != nil {
		channelStats = s.config.NotificationChannelsDiagnostics()
	}
	logStats := logger.GlobalBroadcaster.Diagnostics()
	nativeStats := native.Diagnostics{}
	if s.config.NativeUIDiagnostics != nil {
		nativeStats = s.config.NativeUIDiagnostics()
	}

	smsRunning := s.config.SMS != nil && s.config.SMS.Running()
	networkRunning := s.config.Network != nil && s.config.Network.Running()
	callsRunning := s.config.Extras != nil && s.config.Extras.Running()
	vowifiRunning := s.config.VoWiFi != nil && s.config.VoWiFi.Running()
	remoteState := workerState(channelStats.Running && len(channelStats.CommandListeners) > 0)
	if len(channelStats.Recovering) > 0 && remoteState != "running" {
		remoteState = "recovering"
	}
	workers := []workerDiagnostics{
		{ID: "runtime-scan", Name: "Device discovery", Kind: "poller", State: workerState(runtimeStats.Running), Detail: "Discovers the managed modem and owns backend lifecycle.", IntervalMS: runtimeStats.PollIntervalMS, EventSource: true, EventTypes: []string{"device.status.changed", "device.offline"}},
		{ID: "backend-events", Name: "Backend event consumer", Kind: "consumer", State: workerState(runtimeStats.BackendEventConsumers > 0), Detail: fmt.Sprintf("%d active backend event stream(s).", runtimeStats.BackendEventConsumers), EventSource: true, EventTypes: []string{"backend.*", "sim.updated", "sms.updated", "esim.updated", "network.updated", "vowifi.updated"}},
		{ID: "backend-io", Name: "Backend I/O", Kind: "transport", State: workerState(runtimeStats.BackendAttached), Detail: "Owns modem command queues, transport reads and URC/indication dispatch."},
		{ID: "sms-poller", Name: "SMS refresh", Kind: "poller", State: workerState(smsRunning), Detail: "Refreshes modem storage and publishes new-message events.", IntervalMS: 3000, EventSource: true, EventTypes: []string{"sms.received", "sms.updated"}},
		{ID: "network-poller", Name: "Network status", Kind: "poller", State: workerState(networkRunning), Detail: "Refreshes radio registration and signal state.", IntervalMS: 15000, EventSource: true, EventTypes: []string{"network.updated"}},
		{ID: "traffic-poller", Name: "Traffic sampler", Kind: "poller", State: workerState(networkRunning), Detail: "Samples interface counters; unchanged samples are suppressed.", IntervalMS: 1000, EventSource: true, EventTypes: []string{"traffic.updated"}},
		{ID: "call-poller", Name: "Call monitor", Kind: "poller", State: workerState(callsRunning), Detail: "Reads CLCC and emits call lifecycle events.", IntervalMS: 3000, EventSource: true, EventTypes: []string{"call.*"}},
		{ID: "notification-policy", Name: "Notification policy", Kind: "consumer", State: workerState(notificationStats.Running), Detail: "Deduplicates domain events and selects user-facing notifications."},
		{ID: "notification-delivery", Name: "Notification delivery", Kind: "queue", State: workerState(notificationStats.Running), Detail: "Serializes native and remote notification sink calls.", QueueDepth: notificationStats.QueueDepth, QueueCapacity: notificationStats.QueueCapacity, Dropped: notificationStats.Dropped},
		{ID: "vowifi-runtime", Name: "VoWiFi recovery", Kind: "consumer", State: workerState(vowifiRunning), Detail: "Observes device and network events and schedules recovery operations."},
		{ID: "remote-listeners", Name: "Remote command listeners", Kind: "listener", State: remoteState, Detail: fmt.Sprintf("Configured: %v; listening: %v; recovering: %d", channelStats.ConfiguredChannels, channelStats.CommandListeners, len(channelStats.Recovering))},
		{ID: "native-commands", Name: "Native UI commands", Kind: "consumer", State: workerState(nativeStats.Running), Detail: "Consumes commands from the macOS native UI.", QueueDepth: nativeStats.QueueDepth, QueueCapacity: nativeStats.QueueCapacity, Dropped: nativeStats.Dropped},
		{ID: "operations", Name: "Async operations", Kind: "dynamic", State: map[bool]string{true: "running", false: "idle"}[operationStats.Active > 0], Detail: fmt.Sprintf("%d active operation(s); admission=%t.", operationStats.Active, operationStats.Accepting)},
	}

	channels := []channelDiagnostics{
		{ID: "domain-events", Name: "Domain EventBus", Kind: "fan-out", State: workerState(runtimeStats.Running), Detail: "The shared in-process event stream used by operations, notifications, VoWiFi and browser sessions.", Published: bus.Published, Dropped: bus.CumulativeDrops, Subscribers: len(bus.Subscribers)},
		{ID: "backend-events", Name: "Backend device events", Kind: "point-to-point", State: workerState(runtimeStats.BackendEventConsumers > 0), Detail: "AT/QMI backend events translated into domain events.", Subscribers: runtimeStats.BackendEventConsumers},
		{ID: "notification-queue", Name: "Notification sink queue", Kind: "bounded queue", State: workerState(notificationStats.Running), Detail: "Decouples event policy from native and remote delivery.", Dropped: notificationStats.Dropped, QueueDepth: notificationStats.QueueDepth, QueueCapacity: notificationStats.QueueCapacity},
		{ID: "remote-commands", Name: "Remote command streams", Kind: "listeners", State: remoteState, Detail: fmt.Sprintf("Configured: %v; active: %v; recovering: %d", channelStats.ConfiguredChannels, channelStats.Channels, len(channelStats.Recovering)), Published: channelStats.DeliveryAttempts, Dropped: channelStats.DeliveryFailures, Subscribers: len(channelStats.CommandListeners)},
		{ID: "log-broadcast", Name: "Log broadcaster", Kind: "fan-out", State: "running", Detail: "Best-effort live log stream; payloads are not included in this response.", Published: logStats.Published, Dropped: logStats.Dropped, Subscribers: logStats.Subscribers, QueueCapacity: logStats.Capacity},
		{ID: "native-commands", Name: "Native UI command queue", Kind: "bounded queue", State: workerState(nativeStats.Running), Detail: "Carries validated actions from Swift/AppKit to the Go application layer.", Dropped: nativeStats.Dropped, Subscribers: map[bool]int{true: 1, false: 0}[nativeStats.Running], QueueDepth: nativeStats.QueueDepth, QueueCapacity: nativeStats.QueueCapacity},
	}

	response := runtimeDiagnosticsResponse{
		GeneratedAt: time.Now().UTC(), UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		Goroutines: gort.NumGoroutine(), Workers: workers, Channels: channels, EventBus: bus,
		Flows: []flowDiagnostics{
			{ID: "device-state", From: "Device discovery / backend", Via: "Domain EventBus", To: []string{"Browser WebSocket", "Notification policy", "VoWiFi recovery"}, EventTypes: []string{"device.*", "backend.*", "sim.updated", "network.updated"}},
			{ID: "telephony", From: "SMS and call pollers", Via: "Domain EventBus", To: []string{"Browser WebSocket", "Notification policy"}, EventTypes: []string{"sms.received", "call.*"}},
			{ID: "notification", From: "Notification policy", Via: "Notification sink queue", To: []string{"Native UI", "Telegram", "Feishu", "Webhook", "Bark", "Email", "Pushplus"}, EventTypes: []string{"call.incoming", "call.missed", "sms.received", "device.offline"}},
			{ID: "operations", From: "HTTP commands / recovery", Via: "Operation manager + Domain EventBus", To: []string{"Workers", "Browser WebSocket"}, EventTypes: []string{"operation.changed", "operation.progress", "operation.log", "operation.completed"}},
		},
		Topology:         runtimeTopology(workers, channels, channelStats),
		Traces:           busTraces(s.config.Runtime),
		ChannelRecovery:  channelStats.Recovering,
		RoutePerformance: s.metrics.summaries(),
		Snapshots:        s.snapshotDiagnostics(),
	}
	writeJSON(w, nethttp.StatusOK, response)
}

func (s *Server) snapshotDiagnostics() []snapshot.Summary {
	var out []snapshot.Summary
	if s.config.Device != nil {
		out = append(out, s.config.Device.SnapshotDiagnostics()...)
	}
	if s.config.ESIM != nil {
		out = append(out, s.config.ESIM.SnapshotDiagnostics()...)
	}
	if s.config.DeviceControl != nil {
		out = append(out, s.config.DeviceControl.SnapshotDiagnostics()...)
	}
	if s.config.Network != nil {
		out = append(out, s.config.Network.SnapshotDiagnostics()...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func busTraces(r *appRuntime.Runtime) []appRuntime.MessageTrace {
	if r == nil {
		return []appRuntime.MessageTrace{}
	}
	return r.Events().RecentMessageTraces()
}

func runtimeTopology(workers []workerDiagnostics, channels []channelDiagnostics, channelStats notify.Diagnostics) topologyDiagnostics {
	states := map[string]string{}
	details := map[string]string{}
	for _, worker := range workers {
		states[worker.ID], details[worker.ID] = worker.State, worker.Detail
	}
	for _, channel := range channels {
		states[channel.ID], details[channel.ID] = channel.State, channel.Detail
	}
	active := map[string]bool{}
	for _, name := range channelStats.Channels {
		active[strings.ToLower(name)] = true
	}
	configured := map[string]bool{}
	for _, name := range channelStats.ConfiguredChannels {
		configured[strings.ToLower(name)] = true
	}
	recovering := map[string]notify.ChannelRecovery{}
	for _, recovery := range channelStats.Recovering {
		recovering[strings.ToLower(recovery.Channel)] = recovery
	}
	node := func(id, name, kind, fallbackState string) topologyNodeDiagnostics {
		state := states[id]
		if state == "" {
			state = fallbackState
		}
		return topologyNodeDiagnostics{ID: id, Name: name, Kind: kind, State: state, Detail: details[id]}
	}
	nodes := []topologyNodeDiagnostics{
		node("runtime-scan", "Device discovery", "source", "stopped"),
		node("backend-events", "Backend event stream", "source", "stopped"),
		node("sms-poller", "SMS poller", "source", "stopped"),
		node("network-poller", "Network poller", "source", "stopped"),
		node("traffic-poller", "Traffic sampler", "source", "stopped"),
		node("call-poller", "Call poller", "source", "stopped"),
		node("operations", "HTTP / operations", "source", "idle"),
		node("application", "Application services", "source", "running"),
		node("domain-events", "Domain EventBus", "channel", "running"),
		node("notification-policy", "Notification policy", "processor", "stopped"),
		node("notification-queue", "Notification queue", "channel", "stopped"),
		node("vowifi-runtime", "VoWiFi recovery", "processor", "stopped"),
		node("browser-websocket", "Browser WebSocket", "destination", "running"),
		node("native-ui", "Native UI", "destination", states["native-commands"]),
	}
	for _, name := range []string{"telegram", "feishu", "webhook", "bark", "email", "pushplus"} {
		state := "stopped"
		detail := "Not configured."
		if configured[name] {
			detail = "Configured but inactive."
		}
		if active[name] {
			state = "running"
			detail = "Configured and active."
		} else if recovery, ok := recovering[name]; ok {
			if recovery.Retryable {
				state = "recovering"
				detail = fmt.Sprintf("Recovery attempt %d; next retry %s.", recovery.Attempts, recovery.NextRetry.Format(time.RFC3339))
			} else {
				detail = fmt.Sprintf("Configuration failed: %s", recovery.LastError)
			}
		}
		nodes = append(nodes, topologyNodeDiagnostics{ID: name, Name: strings.ToUpper(name[:1]) + name[1:], Kind: "destination", State: state, Detail: detail})
	}
	edge := func(source, target string, eventTypes ...string) topologyEdgeDiagnostics {
		return topologyEdgeDiagnostics{ID: source + "--" + target, Source: source, Target: target, EventTypes: eventTypes}
	}
	edges := []topologyEdgeDiagnostics{
		edge("runtime-scan", "domain-events", "device.status.changed", "device.offline"),
		edge("backend-events", "domain-events", "backend.*", "sim.*", "sms.*", "esim.*", "network.*", "vowifi.*"),
		edge("sms-poller", "domain-events", "sms.*"),
		edge("network-poller", "domain-events", "network.*"),
		edge("traffic-poller", "domain-events", "traffic.*"),
		edge("call-poller", "domain-events", "call.*"),
		edge("operations", "domain-events", "operation.*", "esim.*"),
		edge("application", "domain-events", "*"),
		edge("domain-events", "browser-websocket", "*"),
		edge("domain-events", "notification-policy", "call.*", "sms.received", "device.offline", "network.updated"),
		edge("domain-events", "vowifi-runtime", "device.*", "network.*", "backend.*"),
		edge("notification-policy", "notification-queue", "approved notifications"),
		edge("notification-queue", "native-ui", "approved notifications"),
	}
	for _, name := range []string{"telegram", "feishu", "webhook", "bark", "email", "pushplus"} {
		edges = append(edges, edge("notification-queue", name, "user-facing notifications"))
	}
	return topologyDiagnostics{Nodes: nodes, Edges: edges}
}

func (s *Server) runtimeTraces(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"traces": busTraces(s.config.Runtime)})
}

func (s *Server) runtimeTraceByID(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/traces/")
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || s.config.Runtime == nil {
		nethttp.NotFound(w, r)
		return
	}
	trace, ok := s.config.Runtime.Events().MessageTrace(id)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}
	writeJSON(w, nethttp.StatusOK, trace)
}

func (s *Server) runtimeTraceStream(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Runtime == nil {
		nethttp.Error(w, "runtime unavailable", nethttp.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		nethttp.Error(w, "streaming unsupported", nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()
	sub := s.config.Runtime.Events().SubscribeMessageTraces(32)
	defer sub.Unsubscribe()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	// A single event commonly records several synchronous hops. Coalesce those
	// snapshots by trace ID so clients receive the newest path once instead of
	// the complete, growing trace after every hop.
	flush := time.NewTicker(25 * time.Millisecond)
	defer flush.Stop()
	pending := make(map[uint64]appRuntime.MessageTrace)
	pendingOrder := make([]uint64, 0, 8)
	writeTrace := func(trace appRuntime.MessageTrace) bool {
		payload, err := json.Marshal(trace)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: trace\ndata: %s\n\n", trace.ID, payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case trace := <-sub.Updates:
			if _, exists := pending[trace.ID]; !exists {
				pendingOrder = append(pendingOrder, trace.ID)
			}
			pending[trace.ID] = trace
		case <-flush.C:
			for _, id := range pendingOrder {
				trace := pending[id]
				if !writeTrace(trace) {
					return
				}
				delete(pending, id)
			}
			pendingOrder = pendingOrder[:0]
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
