package httpapi

import (
	"bytes"
	"errors"
	"log"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouteRegistryInvariants(t *testing.T) {
	specs := routeRegistry()
	seen := make(map[string]bool)
	multi := make(map[string]map[string]bool)
	for _, spec := range specs {
		key := spec.Method + " " + spec.Pattern
		if seen[key] {
			t.Fatalf("duplicate route %s", key)
		}
		seen[key] = true
		if spec.Method == "" || spec.Pattern == "" || !strings.HasPrefix(spec.Pattern, "/api/v1/") || spec.Handler == nil || spec.Operation == nil {
			t.Fatalf("incomplete route: %#v", spec)
		}
		policy, ok := workloadPolicies[spec.Workload]
		if !ok {
			t.Fatalf("route %s has unknown workload %q", key, spec.Workload)
		}
		if spec.Stream == NoStream && policy.Deadline <= 0 {
			t.Fatalf("route %s has no deadline", key)
		}
		if spec.Stream != NoStream && (spec.Workload != StreamWorkload || policy.Deadline != 0) {
			t.Fatalf("stream route %s has normal deadline", key)
		}
		if multi[spec.Pattern] == nil {
			multi[spec.Pattern] = make(map[string]bool)
		}
		multi[spec.Pattern][spec.Method] = true
	}
	for _, key := range []string{
		"GET /api/v1/runtime/traces/{trace_id}",
		"POST /api/v1/esim/notifications/{sequence}/process",
		"DELETE /api/v1/esim/notifications/{sequence}",
		"POST /api/v1/esim/operations/{operation_id}/confirmation-code",
		"PUT /api/v1/sim-profiles/{iccid}",
		"DELETE /api/v1/sim-profiles/{iccid}",
		"GET /api/v1/operations/{operation_id}",
	} {
		if !seen[key] {
			t.Errorf("missing parameter route %s", key)
		}
	}
	for _, path := range []string{"/api/v1/sim-profiles", "/api/v1/vowifi/proxies", "/api/v1/notifications/channels", "/api/v1/settings/startup"} {
		if len(multi[path]) < 2 {
			t.Errorf("%s is not represented as multi-method: %v", path, multi[path])
		}
	}
}

func TestOpenAPIHasExactlyOneOperationPerRegisteredRoute(t *testing.T) {
	paths := openAPIDocument()["paths"].(map[string]any)
	want := make(map[string]bool)
	for _, spec := range routeRegistry() {
		key := spec.Method + " " + spec.Pattern
		want[key] = true
		operations, ok := paths[spec.Pattern].(map[string]any)
		if !ok || operations[openAPIMethod(spec.Method)] == nil {
			t.Errorf("OpenAPI missing %s", key)
		}
		if strings.Contains(spec.Pattern, "{") {
			operation := operations[openAPIMethod(spec.Method)].(map[string]any)
			if len(operation["parameters"].([]any)) == 0 {
				t.Errorf("OpenAPI path parameters missing for %s", key)
			}
		}
	}
	got := 0
	for path, value := range paths {
		for method := range value.(map[string]any) {
			got++
			key := strings.ToUpper(method) + " " + path
			if !want[key] {
				t.Errorf("OpenAPI has unregistered operation %s", key)
			}
		}
	}
	if got != len(want) {
		t.Fatalf("OpenAPI operations=%d routes=%d", got, len(want))
	}
}

func TestRouteDeadlineAndStreamPolicy(t *testing.T) {
	server := &Server{metrics: newRouteMetrics()}
	normal := RouteSpec{Method: nethttp.MethodGet, Pattern: "/api/v1/test/{id}", Workload: MemoryRead, Stream: NoStream}
	var deadline time.Time
	handler := server.serveRoute(normal, nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		deadline, _ = r.Context().Deadline()
		w.WriteHeader(nethttp.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/api/v1/test/private", nil))
	if deadline.IsZero() || time.Until(deadline) > workloadPolicies[MemoryRead].Deadline {
		t.Fatalf("deadline = %v", deadline)
	}

	stream := RouteSpec{Method: nethttp.MethodGet, Pattern: "/api/v1/test/ws", Workload: StreamWorkload, Stream: WebSocket}
	streamDeadline := true
	server.serveRoute(stream, nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) { _, streamDeadline = r.Context().Deadline() })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, stream.Pattern, nil))
	if streamDeadline {
		t.Fatal("stream received a normal request deadline")
	}
}

func TestRouteSummaryAndLogsExcludeRequestAndErrorData(t *testing.T) {
	server := &Server{metrics: newRouteMetrics()}
	spec := RouteSpec{Method: nethttp.MethodPut, Pattern: "/api/v1/sim-profiles/{iccid}", Workload: LocalCommand, Stream: NoStream}
	secret := "sensitive-iccid-value"
	rawError := "private backend response"
	request := httptest.NewRequest(nethttp.MethodPut, "/api/v1/sim-profiles/"+secret+"?token=private-query", strings.NewReader(`{"command":"private-command","body":"private-body"}`))
	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldWriter) })
	server.serveRoute(spec, nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		writeError(w, errors.New(rawError))
	})).ServeHTTP(httptest.NewRecorder(), request)

	output := logs.String()
	for _, forbidden := range []string{secret, "private-query", "private-command", "private-body", rawError} {
		if strings.Contains(output, forbidden) {
			t.Errorf("completion log contains %q: %s", forbidden, output)
		}
	}
	for _, required := range []string{"route=/api/v1/sim-profiles/{iccid}", "workload=local_command", "error_code=internal"} {
		if !strings.Contains(output, required) {
			t.Errorf("completion log missing %q: %s", required, output)
		}
	}
	server.metrics.mu.Lock()
	defer server.metrics.mu.Unlock()
	if len(server.metrics.values) != 1 {
		t.Fatalf("metric series=%d", len(server.metrics.values))
	}
	for key := range server.metrics.values {
		joined := key.Method + key.Pattern + string(key.Workload) + key.StatusClass + key.ErrorCode
		for _, forbidden := range []string{secret, "private-query", "private-command", rawError} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("metric key contains %q: %#v", forbidden, key)
			}
		}
	}
}

func TestRouteMetricsRemainBoundedByRegistryDimensions(t *testing.T) {
	metrics := newRouteMetrics()
	spec := RouteSpec{Method: nethttp.MethodGet, Pattern: "/api/v1/operations/{operation_id}", Workload: MemoryRead}
	for range 10000 {
		metrics.record(spec, nethttp.StatusOK, "", 10, time.Millisecond)
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if len(metrics.values) != 1 {
		t.Fatalf("metric series=%d", len(metrics.values))
	}
	for _, value := range metrics.values {
		if value.Count != 10000 {
			t.Fatalf("count=%d", value.Count)
		}
	}
}

func TestRegistryPreservesStructuredMethodErrors(t *testing.T) {
	server := newTestServer(t, nil)
	recorder := httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(nethttp.MethodPatch, "/api/v1/device/status", nil))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != nethttp.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) || recorder.Header().Get("Allow") != nethttp.MethodGet {
		t.Fatalf("headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
}
