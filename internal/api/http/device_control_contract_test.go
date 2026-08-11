package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/operation"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

// waitTerminal 等待异步操作到达终态, 使 busy 互斥与操作真实生命周期一致。
func waitTerminal(t *testing.T, ops *operation.Manager, operationID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, ok := ops.Get(operationID)
		if ok && (status.State == operation.Succeeded || status.State == operation.Failed || status.State == operation.Cancelled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("operation %s did not reach a terminal state", operationID)
}

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

func TestDeviceControlBusyMutexContract(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{
		StableID: "busy-device", PhysicalLocation: "usb/1-2",
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

	// 空闲设备接受操作: 无需任何前置租约/token 请求。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/actions/adb-unlock", nil)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("idle action status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var first struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &first); err != nil || first.OperationID == "" {
		t.Fatalf("idle action response=%s err=%v", recorder.Body.String(), err)
	}
	waitTerminal(t, ops, first.OperationID)

	// 一个操作持有时, 第二个操作/请求被拒绝为 busy。
	finish, err := service.BeginControlOperation("test.operation")
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/actions/adb-unlock", nil)))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "device_session_conflict") {
		t.Fatalf("busy action status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// 状态响应携带 active_operation, 前端据此显示设备忙。
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/device-control", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, statusRequest)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"active_operation":"test.operation"`) {
		t.Fatalf("busy status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// shell 打开同样被 busy 拒绝。
	shellRequest := withSameOrigin(httptest.NewRequest(http.MethodGet, "/api/v1/device-control/actions/adb/shell/ws?serial=test", nil))
	shellRequest.Header.Set("Connection", "Upgrade")
	shellRequest.Header.Set("Upgrade", "websocket")
	shellRequest.Header.Set("Sec-WebSocket-Version", "13")
	shellRequest.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, shellRequest)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "device_session_conflict") {
		t.Fatalf("shell while busy status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	finish()
	// 操作结束后设备恢复空闲。
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device-control/actions/adb-unlock", nil)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("post-busy action status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
