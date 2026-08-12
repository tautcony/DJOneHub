package httpapi

import (
	"context"
	"log"
	nethttp "net/http"
	"sort"
	"strings"
	"sync"
	"time"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

type WorkloadClass string
type StreamKind string
type HandlerRef func(*Server) nethttp.HandlerFunc
type OpenAPIOperation map[string]any

const (
	MemoryRead     WorkloadClass = "memory_read"
	StorageRead    WorkloadClass = "storage_read"
	DeviceRead     WorkloadClass = "device_read"
	FullDeviceRead WorkloadClass = "full_device_read"
	LocalCommand   WorkloadClass = "local_command"
	AsyncAccept    WorkloadClass = "async_accept"
	ExternalProbe  WorkloadClass = "external_probe"
	StreamWorkload WorkloadClass = "stream"

	NoStream  StreamKind = "none"
	WebSocket StreamKind = "websocket"
	SSE       StreamKind = "sse"
)

type RouteSpec struct {
	Method    string
	Pattern   string
	Workload  WorkloadClass
	Stream    StreamKind
	Handler   HandlerRef
	Operation OpenAPIOperation
}

type WorkloadPolicy struct{ Deadline time.Duration }

var workloadPolicies = map[WorkloadClass]WorkloadPolicy{
	MemoryRead: {Deadline: 5 * time.Second}, StorageRead: {Deadline: 5 * time.Second},
	DeviceRead: {Deadline: 30 * time.Second}, FullDeviceRead: {Deadline: 45 * time.Second},
	LocalCommand: {Deadline: 30 * time.Second}, AsyncAccept: {Deadline: 5 * time.Second},
	ExternalProbe: {Deadline: 45 * time.Second}, StreamWorkload: {},
}

func bind(handler func(*Server, nethttp.ResponseWriter, *nethttp.Request)) HandlerRef {
	return func(server *Server) nethttp.HandlerFunc {
		return func(w nethttp.ResponseWriter, r *nethttp.Request) { handler(server, w, r) }
	}
}

func readOperation(description string) OpenAPIOperation {
	return OpenAPIOperation{"responses": responses(description)}
}

func commandOperation(description string, status string) OpenAPIOperation {
	return OpenAPIOperation{"responses": map[string]any{
		status: map[string]any{"description": description},
		"400":  map[string]any{"$ref": "#/components/responses/Error"},
		"409":  map[string]any{"$ref": "#/components/responses/Error"},
		"422":  map[string]any{"$ref": "#/components/responses/Error"},
		"503":  map[string]any{"$ref": "#/components/responses/Error"},
	}}
}

func streamOperation(description string) OpenAPIOperation {
	return OpenAPIOperation{"responses": map[string]any{
		"101": map[string]any{"description": description},
		"200": map[string]any{"description": description},
		"401": map[string]any{"$ref": "#/components/responses/Error"},
	}}
}

func routeRegistry() []RouteSpec {
	get := func(path string, workload WorkloadClass, handler HandlerRef, description string) RouteSpec {
		return RouteSpec{Method: nethttp.MethodGet, Pattern: path, Workload: workload, Stream: NoStream, Handler: handler, Operation: readOperation(description)}
	}
	post := func(path string, workload WorkloadClass, handler HandlerRef, description, status string) RouteSpec {
		return RouteSpec{Method: nethttp.MethodPost, Pattern: path, Workload: workload, Stream: NoStream, Handler: handler, Operation: commandOperation(description, status)}
	}
	write := func(method, path string, workload WorkloadClass, handler HandlerRef, description string) RouteSpec {
		return RouteSpec{Method: method, Pattern: path, Workload: workload, Stream: NoStream, Handler: handler, Operation: commandOperation(description, "200")}
	}
	return []RouteSpec{
		get("/api/v1/device", DeviceRead, bind((*Server).deviceStatus), "single-device status"),
		get("/api/v1/device/capabilities", MemoryRead, bind((*Server).deviceCapabilities), "device capabilities"),
		get("/api/v1/device/status", DeviceRead, bind((*Server).deviceStatus), "single-device status"),
		post("/api/v1/device/actions/rescan", LocalCommand, bind((*Server).rescan), "rescan result", "200"),
		post("/api/v1/device/actions/reboot", AsyncAccept, bind((*Server).reboot), "operation accepted", "202"),
		get("/api/v1/runtime/diagnostics", MemoryRead, bind((*Server).runtimeDiagnostics), "runtime diagnostics"),
		{Method: nethttp.MethodGet, Pattern: "/api/v1/runtime/traces/stream", Workload: StreamWorkload, Stream: SSE, Handler: bind((*Server).runtimeTraceStream), Operation: streamOperation("runtime trace stream")},
		get("/api/v1/runtime/traces/{trace_id}", MemoryRead, bind((*Server).runtimeTraceByID), "runtime message trace"),
		get("/api/v1/runtime/traces", MemoryRead, bind((*Server).runtimeTraces), "recent runtime message traces"),
		post("/api/v1/sms/actions/refresh", DeviceRead, bind((*Server).smsRefresh), "SMS list", "200"),
		post("/api/v1/sms/actions/send", AsyncAccept, bind((*Server).smsSend), "operation accepted", "202"),
		post("/api/v1/sms/actions/clear", DeviceRead, bind((*Server).smsClear), "SMS clear result", "200"),
		get("/api/v1/esim", FullDeviceRead, bind((*Server).esimOverview), "eSIM profiles"),
		post("/api/v1/esim/actions/download", AsyncAccept, bind((*Server).esimDownload), "operation accepted", "202"),
		post("/api/v1/esim/actions/enable", AsyncAccept, bind((*Server).esimEnable), "operation accepted", "202"),
		post("/api/v1/esim/actions/rename", FullDeviceRead, bind((*Server).esimRename), "eSIM rename result", "200"),
		post("/api/v1/esim/actions/delete", AsyncAccept, bind((*Server).esimDelete), "operation accepted", "202"),
		post("/api/v1/esim/actions/disable", AsyncAccept, bind((*Server).esimDisable), "operation accepted", "202"),
		get("/api/v1/esim/notifications", FullDeviceRead, bind((*Server).esimNotifications), "pending eUICC notifications"),
		get("/api/v1/esim/notifications/history", StorageRead, bind((*Server).esimNotificationBySequence), "local eUICC notification history"),
		post("/api/v1/esim/notifications/{sequence}/process", FullDeviceRead, bind((*Server).esimNotificationBySequence), "notification retry result", "200"),
		write(nethttp.MethodDelete, "/api/v1/esim/notifications/{sequence}", FullDeviceRead, bind((*Server).esimNotificationBySequence), "notification removal result"),
		post("/api/v1/esim/operations/{operation_id}/confirmation-code", MemoryRead, bind((*Server).esimConfirmationCodeReply), "confirmation code accepted", "200"),
		get("/api/v1/network", DeviceRead, bind((*Server).networkStatus), "network status"),
		post("/api/v1/network/actions/mode", AsyncAccept, bind((*Server).networkMode), "operation accepted", "202"),
		post("/api/v1/network/actions/check", ExternalProbe, bind((*Server).networkCheck), "connectivity result", "200"),
		get("/api/v1/network/actions/traffic", MemoryRead, bind((*Server).networkTraffic), "network traffic"),
		get("/api/v1/network/traffic/daily", StorageRead, bind((*Server).networkTrafficDaily), "daily network traffic"),
		get("/api/v1/network/traffic/range", StorageRead, bind((*Server).networkTrafficRange), "network traffic range"),
		get("/api/v1/network/diagnostics", FullDeviceRead, bind((*Server).networkDiagnostics), "network diagnostics"),
		post("/api/v1/device/actions/raw-at", DeviceRead, bind((*Server).rawAT), "raw AT response", "200"),
		get("/api/v1/device-control", FullDeviceRead, bind((*Server).deviceControlStatus), "device control status"),
		post("/api/v1/device-control/settings", LocalCommand, bind((*Server).deviceControlSettings), "device control settings", "200"),
		post("/api/v1/device-control/actions/adb-unlock", AsyncAccept, bind((*Server).deviceControlADBUnlock), "ADB unlock operation", "202"),
		post("/api/v1/device-control/actions/adb-mode", AsyncAccept, bind((*Server).deviceControlADBMode), "ADB mode operation", "202"),
		post("/api/v1/device-control/actions/adb/reboot", AsyncAccept, bind((*Server).deviceControlADBReboot), "ADB reboot operation", "202"),
		{Method: nethttp.MethodGet, Pattern: "/api/v1/device-control/actions/adb/shell/ws", Workload: StreamWorkload, Stream: WebSocket, Handler: bind((*Server).deviceControlADBShellWS), Operation: streamOperation("interactive ADB shell")},
		post("/api/v1/device-control/actions/usb-id", AsyncAccept, bind((*Server).deviceControlUSBID), "USB ID operation", "202"),
		post("/api/v1/device-control/actions/edl", AsyncAccept, bind((*Server).deviceControlEDL), "EDL entry operation", "202"),
		post("/api/v1/device-control/actions/nand-backup", AsyncAccept, bind((*Server).deviceControlBackup), "NAND backup operation", "202"),
		post("/api/v1/device-control/actions/select-backup-directory", LocalCommand, bind((*Server).deviceControlBackupSelectDirectory), "backup directory selection", "200"),
		post("/api/v1/device-control/actions/select-edl-directory", LocalCommand, bind((*Server).deviceControlBackupSelectEDLDirectory), "EDL directory selection", "200"),
		post("/api/v1/device-control/actions/select-adb-file", LocalCommand, bind((*Server).deviceControlSelectADBFile), "ADB file selection", "200"),
		post("/api/v1/device-control/actions/select-loader-file", LocalCommand, bind((*Server).deviceControlSelectLoaderFile), "loader file selection", "200"),
		post("/api/v1/device-control/actions/reset", AsyncAccept, bind((*Server).deviceControlReset), "restore normal USB mode operation", "202"),
		get("/api/v1/vowifi", MemoryRead, bind((*Server).vowifiStatus), "VoWiFi status"),
		post("/api/v1/vowifi/actions/enable", AsyncAccept, bind((*Server).vowifiEnable), "operation accepted", "202"),
		post("/api/v1/vowifi/actions/disable", AsyncAccept, bind((*Server).vowifiDisable), "operation accepted", "202"),
		post("/api/v1/vowifi/actions/reconnect", AsyncAccept, bind((*Server).vowifiReconnect), "operation accepted", "202"),
		get("/api/v1/vowifi/proxies", StorageRead, bind((*Server).vowifiProxies), "upstream proxy list"),
		post("/api/v1/vowifi/proxies", LocalCommand, bind((*Server).vowifiProxies), "upstream proxy upsert", "200"),
		write(nethttp.MethodDelete, "/api/v1/vowifi/proxies", LocalCommand, bind((*Server).vowifiProxies), "upstream proxy delete"),
		get("/api/v1/vowifi/proxy-country-rules", StorageRead, bind((*Server).vowifiProxyCountryRules), "proxy country rules"),
		post("/api/v1/vowifi/proxy-country-rules", LocalCommand, bind((*Server).vowifiProxyCountryRules), "proxy country rule upsert", "200"),
		write(nethttp.MethodDelete, "/api/v1/vowifi/proxy-country-rules", LocalCommand, bind((*Server).vowifiProxyCountryRules), "proxy country rule delete"),
		get("/api/v1/vowifi/country-table", MemoryRead, bind((*Server).vowifiCountryTable), "MCC country table status"),
		get("/api/v1/vowifi/card-policies", StorageRead, bind((*Server).vowifiCardPolicies), "card VoWiFi policies"),
		write(nethttp.MethodPut, "/api/v1/vowifi/card-policies", LocalCommand, bind((*Server).vowifiCardPolicies), "card VoWiFi policy update"),
		get("/api/v1/notifications/debug", MemoryRead, bind((*Server).notificationDebug), "notifier debug capabilities"),
		post("/api/v1/notifications/debug", LocalCommand, bind((*Server).notificationDebug), "published notifier debug events", "200"),
		get("/api/v1/notifications/permissions", MemoryRead, bind((*Server).notificationPermissions), "notification permission status"),
		post("/api/v1/notifications/permissions/request", LocalCommand, bind((*Server).requestNotificationPermission), "notification permission request", "202"),
		post("/api/v1/notifications/permissions/open-settings", LocalCommand, bind((*Server).openNotificationSettings), "notification settings open", "202"),
		get("/api/v1/notifications/preferences", MemoryRead, bind((*Server).notificationPreferences), "notification preferences"),
		write(nethttp.MethodPut, "/api/v1/notifications/preferences", LocalCommand, bind((*Server).notificationPreferences), "notification preferences update"),
		get("/api/v1/notifications/channels", MemoryRead, bind((*Server).notificationChannels), "notification channel settings"),
		write(nethttp.MethodPut, "/api/v1/notifications/channels", LocalCommand, bind((*Server).notificationChannels), "notification channel settings update"),
		post("/api/v1/notifications/channels/actions/test", ExternalProbe, bind((*Server).testNotificationChannel), "notification channel test", "200"),
		post("/api/v1/notifications/channels/telegram/chat-ids", ExternalProbe, bind((*Server).discoverTelegramChatIDs), "Telegram chat ID discovery", "200"),
		get("/api/v1/settings/startup", MemoryRead, bind((*Server).startupSettings), "login startup status"),
		write(nethttp.MethodPut, "/api/v1/settings/startup", LocalCommand, bind((*Server).startupSettings), "login startup status update"),
		get("/api/v1/calls", MemoryRead, bind((*Server).calls), "call monitor"),
		post("/api/v1/calls/actions/dial", DeviceRead, bind((*Server).callDial), "dial result", "200"),
		post("/api/v1/calls/actions/reject", DeviceRead, bind((*Server).callReject), "call rejection", "200"),
		get("/api/v1/esim/health", FullDeviceRead, bind((*Server).esimHealth), "eSIM health"),
		get("/api/v1/sim-profiles", StorageRead, bind((*Server).simProfiles), "SIM Profile registry"),
		post("/api/v1/sim-profiles", LocalCommand, bind((*Server).simProfiles), "SIM Profile create result", "201"),
		write(nethttp.MethodPut, "/api/v1/sim-profiles/{iccid}", LocalCommand, bind((*Server).simProfileByICCID), "SIM Profile update result"),
		write(nethttp.MethodDelete, "/api/v1/sim-profiles/{iccid}", LocalCommand, bind((*Server).simProfileByICCID), "SIM Profile delete result"),
		get("/api/v1/operations/{operation_id}", MemoryRead, bind((*Server).operationStatus), "operation status"),
		get("/api/v1/openapi.json", MemoryRead, bind((*Server).openapi), "OpenAPI document"),
		{Method: nethttp.MethodGet, Pattern: "/api/v1/events/ws", Workload: StreamWorkload, Stream: WebSocket, Handler: bind((*Server).websocket), Operation: streamOperation("event stream")},
	}
}

var durationBounds = [...]time.Duration{5 * time.Millisecond, 25 * time.Millisecond, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 5 * time.Second, 30 * time.Second}

type routeMetricKey struct {
	Method, Pattern        string
	Workload               WorkloadClass
	StatusClass, ErrorCode string
}
type routeMetricValue struct {
	Count, Bytes uint64
	Durations    [len(durationBounds) + 1]uint64
}
type routeMetrics struct {
	mu     sync.Mutex
	values map[routeMetricKey]routeMetricValue
}

type RoutePerformanceSummary struct {
	Method          string                          `json:"method"`
	Route           string                          `json:"route"`
	Workload        WorkloadClass                   `json:"workload"`
	StatusClass     string                          `json:"status_class"`
	ErrorCode       string                          `json:"error_code,omitempty"`
	Count           uint64                          `json:"count"`
	ResponseBytes   uint64                          `json:"response_bytes"`
	DurationBuckets [len(durationBounds) + 1]uint64 `json:"duration_buckets"`
}

func newRouteMetrics() *routeMetrics {
	return &routeMetrics{values: make(map[routeMetricKey]routeMetricValue)}
}
func (m *routeMetrics) record(spec RouteSpec, status int, code string, bytes int, duration time.Duration) {
	key := routeMetricKey{spec.Method, spec.Pattern, spec.Workload, statusClass(status), code}
	m.mu.Lock()
	value := m.values[key]
	value.Count++
	value.Bytes += uint64(bytes)
	bucket := len(durationBounds)
	for i, bound := range durationBounds {
		if duration <= bound {
			bucket = i
			break
		}
	}
	value.Durations[bucket]++
	m.values[key] = value
	m.mu.Unlock()
}

func (m *routeMetrics) summaries() []RoutePerformanceSummary {
	m.mu.Lock()
	out := make([]RoutePerformanceSummary, 0, len(m.values))
	for key, value := range m.values {
		out = append(out, RoutePerformanceSummary{Method: key.Method, Route: key.Pattern, Workload: key.Workload, StatusClass: key.StatusClass, ErrorCode: key.ErrorCode, Count: value.Count, ResponseBytes: value.Bytes, DurationBuckets: value.Durations})
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Method + out[i].Route + out[i].StatusClass + out[i].ErrorCode
		right := out[j].Method + out[j].Route + out[j].StatusClass + out[j].ErrorCode
		return left < right
	})
	return out
}

func statusClass(status int) string {
	if status < 100 {
		return "0xx"
	}
	return string(rune('0'+status/100)) + "xx"
}

func (s *Server) serveRoute(spec RouteSpec, next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		ctx := r.Context()
		if spec.Stream == NoStream {
			policy := workloadPolicies[spec.Workload]
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, policy.Deadline)
			defer cancel()
			r = r.WithContext(ctx)
		}
		started := time.Now()
		recorder := newStatusResponseWriter(w, r.Method, spec.Pattern)
		next.ServeHTTP(recorder, r)
		status := recorder.statusCode()
		duration := time.Since(started)
		s.metrics.record(spec, status, recorder.errorCode, recorder.bytes, duration)
		log.Printf("http request completed method=%s route=%s workload=%s status_class=%s error_code=%s response_bytes=%d duration_ms=%d", spec.Method, spec.Pattern, spec.Workload, statusClass(status), recorder.errorCode, recorder.bytes, duration.Milliseconds())
	})
}

func sortedRouteSpecs() []RouteSpec {
	specs := routeRegistry()
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Pattern == specs[j].Pattern {
			return specs[i].Method < specs[j].Method
		}
		return specs[i].Pattern < specs[j].Pattern
	})
	return specs
}
func muxPattern(spec RouteSpec) string   { return spec.Method + " " + spec.Pattern }
func openAPIMethod(method string) string { return strings.ToLower(method) }

func registryMethodGuard(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		allowed := make(map[string]bool)
		methodMatched := false
		for _, spec := range routeRegistry() {
			if routePathMatches(spec.Pattern, r.URL.Path) {
				allowed[spec.Method] = true
				methodMatched = methodMatched || spec.Method == r.Method
			}
		}
		if len(allowed) > 0 && !methodMatched {
			methods := make([]string, 0, len(allowed))
			for method := range allowed {
				methods = append(methods, method)
			}
			sort.Strings(methods)
			allow := strings.Join(methods, ", ")
			w.Header().Set("Allow", allow)
			writeError(w, derrors.New(derrors.InvalidRequest, "method not allowed", false, map[string]any{"method": allow}))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func routePathMatches(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for index := range patternParts {
		if strings.HasPrefix(patternParts[index], "{") && strings.HasSuffix(patternParts[index], "}") {
			continue
		}
		if patternParts[index] != pathParts[index] {
			return false
		}
	}
	return true
}
