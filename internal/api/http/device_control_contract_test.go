package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/operation"
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
