package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNotificationDebugExposesEventDropCounters verifies the existing
// notification-debug response exposes the cumulative and active-subscriber
// drop counters, and that unsubscribe removes only the active-subscriber
// state while the cumulative count stays monotonic.
func TestNotificationDebugExposesEventDropCounters(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	bus := server.config.Runtime.Events()

	// A slow subscriber that never drains: the second publish is dropped.
	_, _, unsubscribe := bus.Subscribe(1)
	bus.Publish("a", nil)
	bus.Publish("b", nil)

	fetch := func() map[string]any {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/debug", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("debug status = %d", recorder.Code)
		}
		var body struct {
			EventDrops map[string]any `json:"event_drops"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.EventDrops == nil {
			t.Fatal("event_drops missing from debug response")
		}
		return body.EventDrops
	}

	first := fetch()
	if cumulative := first["cumulative"].(float64); cumulative != 1 {
		t.Fatalf("cumulative = %v, want 1", cumulative)
	}
	active, ok := first["active_subscribers"].(map[string]any)
	if !ok || len(active) != 1 {
		t.Fatalf("active_subscribers = %v, want 1 entry", first["active_subscribers"])
	}

	unsubscribe()
	second := fetch()
	if cumulative := second["cumulative"].(float64); cumulative != 1 {
		t.Fatalf("cumulative after unsubscribe = %v, want 1 (monotonic)", cumulative)
	}
	if active := second["active_subscribers"].(map[string]any); len(active) != 0 {
		t.Fatalf("active_subscribers after unsubscribe = %v, want empty", active)
	}
}

func TestRuntimeDiagnosticsExposeBoundedPerformancePolicies(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	server.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/device/status", nil))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/runtime/diagnostics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Routes    []RoutePerformanceSummary `json:"route_performance"`
		Snapshots []struct {
			Name     string            `json:"name"`
			TTLMS    int64             `json:"ttl_ms"`
			Outcomes map[string]uint64 `json:"outcomes"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Routes) == 0 || len(body.Snapshots) != 7 {
		t.Fatalf("routes=%d snapshots=%#v", len(body.Routes), body.Snapshots)
	}
	foundRoute, foundPending := false, false
	for _, route := range body.Routes {
		if route.Route == "/api/v1/device/status" {
			foundRoute = true
		}
	}
	for _, item := range body.Snapshots {
		if len(item.Outcomes) != 4 {
			t.Errorf("snapshot %s outcomes=%v", item.Name, item.Outcomes)
		}
		if item.Name == "esim.pending_notifications" && item.TTLMS == 5000 {
			foundPending = true
		}
	}
	if !foundRoute || !foundPending {
		t.Fatalf("found route=%t pending=%t", foundRoute, foundPending)
	}
}
