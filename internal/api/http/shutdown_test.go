package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdmissionGateRefusesRequestsAfterShutdown verifies the shutdown
// admission gate closes before the HTTP server drains: requests that arrive
// afterwards are refused instead of starting new work.
func TestAdmissionGateRefusesRequestsAfterShutdown(t *testing.T) {
	admitted := true
	server, _ := newReadyServer(t, allContractCapabilities())
	server.config.Admission = func() bool { return admitted }
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)

	response, err := http.Get(ts.URL + "/api/v1/device")
	if err != nil {
		t.Fatalf("get while admitted: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status while admitted = %d, want 200", response.StatusCode)
	}

	admitted = false
	response, err = http.Get(ts.URL + "/api/v1/device")
	if err != nil {
		t.Fatalf("get after admission closed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status after admission closed = %d, want 503", response.StatusCode)
	}
}
