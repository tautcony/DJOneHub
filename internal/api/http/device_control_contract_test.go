package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/operation"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

func TestDeviceControlNamespaceAndSettings(t *testing.T) {
	r, err := runtime.New(runtime.Config{Discovery: emptyDiscovery{}, Backends: emptyFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	ops := operation.NewManager(r.Events())
	store := &settingsJSONStore{}
	service := firmware.NewService(nil, ops, r, firmware.Config{Store: store})
	server := NewServer(Config{DeviceControl: service, Operations: ops, Runtime: r, Auth: AuthenticatorFunc(func(*http.Request) bool { return true }), LoopbackPort: testLoopbackPort})
	handler := server.Handler()

	request := withSameOrigin(httptest.NewRequest(http.MethodGet, "/api/v1/device-control", nil))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("device-control status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var status firmware.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Settings.ADBCommand != "" {
		t.Fatalf("unexpected settings=%+v", status.Settings)
	}

	request = withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/settings", strings.NewReader(`{"adb_command":""}`)))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/firmware", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("legacy firmware route status=%d", recorder.Code)
	}
}

func TestDeviceControlLeaseContract(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{
		StableID: "lease-device", PhysicalLocation: "usb/1-2",
	}}}
	r, err := runtime.New(runtime.Config{Discovery: discovery, Backends: contractFactory{b: &contractBackend{caps: allContractCapabilities()}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops := operation.NewManager(r.Events())
	service := firmware.NewService(nil, ops, r, firmware.Config{})
	server := NewServer(Config{DeviceControl: service, Operations: ops, Runtime: r, Auth: AuthenticatorFunc(func(*http.Request) bool { return true }), LoopbackPort: testLoopbackPort})
	handler := server.Handler()

	acquire := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/session/lease", nil))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, acquire)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("acquire status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var lease struct {
		LeaseToken string `json:"lease_token"`
		Session    struct {
			LeaseOwned bool `json:"lease_owned"`
		} `json:"session"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &lease); err != nil || lease.LeaseToken == "" || !lease.Session.LeaseOwned {
		t.Fatalf("lease response=%s err=%v", recorder.Body.String(), err)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/session/lease", nil)))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "device_session_conflict") {
		t.Fatalf("second acquire status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/device-control", nil)
	statusRequest.Header.Set(deviceControlLeaseHeader, lease.LeaseToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, statusRequest)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"lease_owned":true`) {
		t.Fatalf("owned status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/actions/adb-unlock", nil)))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "device_session_conflict") {
		t.Fatalf("mutation without lease status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	shellRequest := withSameOrigin(httptest.NewRequest(http.MethodGet, "/api/v1/device-control/actions/adb/shell/ws?serial=test", nil))
	shellRequest.Header.Set("Connection", "Upgrade")
	shellRequest.Header.Set("Upgrade", "websocket")
	shellRequest.Header.Set("Sec-WebSocket-Version", "13")
	shellRequest.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, shellRequest)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "device_session_conflict") {
		t.Fatalf("shell without lease status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	finish, err := service.BeginControlOperation(lease.LeaseToken, "test.operation")
	if err != nil {
		t.Fatal(err)
	}
	release := withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/device-control/session/lease", nil))
	release.Header.Set(deviceControlLeaseHeader, lease.LeaseToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, release)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("release during operation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	finish()

	renew := withSameOrigin(httptest.NewRequest(http.MethodPut, "/api/v1/device-control/session/lease", nil))
	renew.Header.Set(deviceControlLeaseHeader, lease.LeaseToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, renew)
	if recorder.Code != http.StatusOK {
		t.Fatalf("renew status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, release)
	if recorder.Code != http.StatusOK {
		t.Fatalf("release status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
