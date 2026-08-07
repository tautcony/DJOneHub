package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/simcards"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/platform/startup"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/transport"
)

type emptyDiscovery struct{}

func (emptyDiscovery) Discover(context.Context) ([]domain.Candidate, error) { return nil, nil }

type emptyFactory struct{}

func (emptyFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return nil, "", nil
}

func newTestServerWithRuntime(t *testing.T, auth Authenticator, r *runtime.Runtime) *Server {
	t.Helper()
	ops := operation.NewManager(r.Events())
	devices := device.NewService(r)
	smsService := sms.NewService(devices, ops, r)
	esimService := esim.NewService(devices, ops, r)
	platformAdapter := unsupported.New("test-offline", domain.CapabilitySet{})
	networkService := network.NewService(devices, ops, r, platformAdapter)
	rawATService := rawat.NewService(devices, r)
	vowifiService := vowifi.NewService(devices, ops, r)
	notificationService := notification.New(notification.Config{Events: r.Events()})
	return NewServer(Config{
		Device: devices, SMS: smsService, ESIM: esimService, Network: networkService,
		Notification: notificationService, RawAT: rawATService, VoWiFi: vowifiService,
		Operations: ops, Runtime: r, Auth: auth, LoopbackPort: testLoopbackPort,
	})
}

// testLoopbackPort anchors the temporary boundary in recorder-based tests,
// which never bind a real listener.
const testLoopbackPort = 7575

// withSameOrigin attaches an allowed loopback Origin so a state-changing test
// request passes the temporary boundary instead of being rejected for missing
// origin metadata.
func withSameOrigin(request *http.Request) *http.Request {
	request.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", testLoopbackPort))
	return request
}

func newTestServer(t *testing.T, auth Authenticator) *Server {
	t.Helper()
	r, err := runtime.New(runtime.Config{Discovery: emptyDiscovery{}, Backends: emptyFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	return newTestServerWithRuntime(t, auth, r)
}

func TestServerReturnsOfflineSnapshot(t *testing.T) {
	server := newTestServer(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/device/status", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Snapshot struct {
			State string `json:"state"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Snapshot.State != string(domain.StateAbsent) {
		t.Fatalf("state = %q", body.Snapshot.State)
	}
}

func TestServerEnforcesConfiguredAuthentication(t *testing.T) {
	server := newTestServer(t, AuthenticatorFunc(func(*http.Request) bool { return false }))
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/rescan", nil))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"]["code"] != string(domainErrorUnauthenticated) {
		t.Fatalf("body = %#v", body)
	}
}

func TestServerManagesLoginStartup(t *testing.T) {
	server := newTestServer(t, nil)
	status := startup.Status{Supported: true}
	server.config.StartupStatus = func() startup.Status { return status }
	server.config.SetStartupEnabled = func(enabled bool) error {
		status.Enabled = enabled
		return nil
	}
	handler := server.Handler()
	read := httptest.NewRecorder()
	handler.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/v1/settings/startup", nil))
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"supported":true`) {
		t.Fatalf("get startup status = %d, %s", read.Code, read.Body.String())
	}
	write := httptest.NewRecorder()
	handler.ServeHTTP(write, withSameOrigin(httptest.NewRequest(http.MethodPut, "/api/v1/settings/startup", strings.NewReader(`{"enabled":true}`))))
	if write.Code != http.StatusOK || !strings.Contains(write.Body.String(), `"enabled":true`) {
		t.Fatalf("put startup status = %d, %s", write.Code, write.Body.String())
	}
}

type contractBackend struct {
	caps domain.CapabilitySet
}

func (b *contractBackend) Mode() string { return "fake" }
func (b *contractBackend) Identity(context.Context) (backend.Identity, error) {
	return backend.Identity{IMEI: "123456789012345", ICCID: "8901000000000000000", Firmware: "test"}, nil
}
func (b *contractBackend) Radio(context.Context) (backend.RadioState, error) {
	return backend.RadioState{Registered: true, Operator: "TestNet", NetworkMode: "LTE", SignalDBM: -71}, nil
}
func (b *contractBackend) SIM(context.Context) (backend.SIMState, error) {
	return backend.SIMState{Inserted: true, ICCID: "8901000000000000000"}, nil
}
func (b *contractBackend) ListSMS(context.Context) ([]backend.SMSMessage, error) {
	return []backend.SMSMessage{{Index: 1, Sender: "+100", Body: "hello"}}, nil
}
func (b *contractBackend) SendSMS(context.Context, string, string) error { return nil }
func (b *contractBackend) USSD(context.Context, string) (string, error)  { return "OK", nil }
func (b *contractBackend) APDU(context.Context, backend.APDURequest) (backend.APDUResponse, error) {
	return backend.APDUResponse{Response: []byte{0x90, 0x00}}, nil
}
func (b *contractBackend) Capabilities(context.Context) domain.CapabilitySet { return b.caps.Clone() }
func (b *contractBackend) Events(context.Context) (<-chan backend.BackendEvent, error) {
	return make(chan backend.BackendEvent), nil
}
func (b *contractBackend) Close() error { return nil }

func (b *contractBackend) ReadSMS(context.Context, backend.NewSMSRef) (backend.SMSMessage, error) {
	return backend.SMSMessage{Index: 1, Sender: "+100", Body: "hello"}, nil
}
func (b *contractBackend) DeleteSMS(context.Context, backend.NewSMSRef) error { return nil }
func (b *contractBackend) DeleteAllSMS(context.Context) error                 { return nil }
func (b *contractBackend) SetInboundSMSHandler(backend.InboundSMSHandler)     {}
func (b *contractBackend) EID(context.Context) (string, error) {
	return "89049032000000000000000000000000", nil
}
func (b *contractBackend) Profiles(context.Context) ([]backend.Profile, error) {
	return []backend.Profile{{ICCID: "8901000000000000000", State: "active"}}, nil
}
func (b *contractBackend) Download(context.Context, string, string, string, *backend.ESIMDownloadOptions) error {
	return nil
}
func (b *contractBackend) Enable(context.Context, string) error  { return nil }
func (b *contractBackend) Disable(context.Context, string) error { return nil }
func (b *contractBackend) Rename(context.Context, string, string) error { return nil }
func (b *contractBackend) Delete(context.Context, string) error   { return nil }
func (b *contractBackend) ListNotifications(context.Context) ([]backend.NotificationItem, error) {
	return []backend.NotificationItem{{SequenceNumber: 1, Event: "install", CanRetry: true}}, nil
}
func (b *contractBackend) ProcessNotification(context.Context, int64) error { return nil }
func (b *contractBackend) RemoveNotification(context.Context, int64) error { return nil }
func (b *contractBackend) RawAT(context.Context, string) (string, error)   { return "OK", nil }

type contractVoWiFiBackend struct{ *contractBackend }

func (b *contractVoWiFiBackend) Enable(context.Context) error  { return nil }
func (b *contractVoWiFiBackend) Disable(context.Context) error { return nil }
func (b *contractVoWiFiBackend) Reconnect(context.Context) error {
	return nil
}
func (b *contractVoWiFiBackend) Status(context.Context) (map[string]any, error) {
	return map[string]any{"available": true, "state": "disabled"}, nil
}

type contractFactory struct{ b backend.ModemBackend }

func (f contractFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return f.b, "fake backend", nil
}

type contractNetwork struct{}

func (contractNetwork) Status(context.Context, domain.Candidate) (transport.NetworkStatus, error) {
	return transport.NetworkStatus{Mode: "usb", Interface: "en0"}, nil
}
func (contractNetwork) SetMode(context.Context, domain.Candidate, string) error { return nil }
func (contractNetwork) CheckConnectivity(context.Context, domain.Candidate) (transport.Connectivity, error) {
	return transport.Connectivity{OK: true, Summary: "reachable"}, nil
}

func newReadyServer(t *testing.T, caps domain.CapabilitySet) (*Server, *operation.Manager) {
	t.Helper()
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "fake-1", Product: "Fake modem"}}}
	b := &contractBackend{caps: caps}
	return newReadyServerWithBackend(t, discovery, b)
}

func newReadyServerWithBackend(t *testing.T, discovery *fakeReadyDiscovery, b backend.ModemBackend) (*Server, *operation.Manager) {
	t.Helper()
	r, err := runtime.New(runtime.Config{Discovery: discovery, Backends: contractFactory{b: b}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops := operation.NewManager(r.Events())
	devices := device.NewService(r)
	smsService := sms.NewService(devices, ops, r)
	esimService := esim.NewService(devices, ops, r)
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "server-test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	simCardsService := simcards.NewService(db)
	networkService := network.NewService(devices, ops, r, contractNetwork{})
	rawATService := rawat.NewService(devices, r)
	vowifiService := vowifi.NewService(devices, ops, r)
	notificationService := notification.New(notification.Config{Events: r.Events()})
	return NewServer(Config{
		Device: devices, SMS: smsService, ESIM: esimService, SimCards: simCardsService,
		Network: networkService, Notification: notificationService, RawAT: rawATService,
		VoWiFi: vowifiService, Operations: ops, Runtime: r, LoopbackPort: testLoopbackPort,
	}), ops
}

type fakeReadyDiscovery struct{ candidate domain.Candidate }

func (d *fakeReadyDiscovery) Discover(context.Context) ([]domain.Candidate, error) {
	return []domain.Candidate{d.candidate}, nil
}

func allContractCapabilities() domain.CapabilitySet {
	return domain.CapabilitySet{
		domain.CapabilityDeviceStatus:   "",
		domain.CapabilitySMSRead:        "",
		domain.CapabilitySMSSend:        "",
		domain.CapabilityESIM:           "",
		domain.CapabilityRawAT:          "",
		domain.CapabilityNetworkStatus:  "",
		domain.CapabilityNetworkControl: "",
	}
}

func TestAPIContractCoversQueriesCommandsAndOperations(t *testing.T) {
	server, ops := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	checks := []struct {
		name string
		path string
	}{
		{name: "device", path: "/api/v1/device"},
		{name: "status", path: "/api/v1/device/status"},
		{name: "capabilities", path: "/api/v1/device/capabilities"},
		{name: "esim", path: "/api/v1/esim"},
		{name: "network", path: "/api/v1/network"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, check.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	recorder := httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/sms/actions/send", strings.NewReader(`{"to":"+100","body":"hello"}`)))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("accepted response = %s", recorder.Body.String())
	}
	if _, ok := ops.Get(accepted.ID); !ok {
		t.Fatalf("operation %q was not registered", accepted.ID)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "/api/v1/esim/actions/download") || !strings.Contains(recorder.Body.String(), "/api/v1/notifications/debug") {
		t.Fatalf("OpenAPI response does not describe all commands: %s", recorder.Body.String())
	}
}

func TestEsimDisableReturnsOperationID(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/disable", strings.NewReader(`{"iccid":"8986012001000000000"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("disable status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("disable response = %s", recorder.Body.String())
	}
}

func TestEsimNotificationsAPI(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"sequence_number":1`) {
		t.Fatalf("notifications list = %d %s", recorder.Code, recorder.Body.String())
	}

	process := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/notifications/1/process", strings.NewReader(`{}`)))
	process.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, process)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"state":"processed"`) {
		t.Fatalf("notification process = %d %s", recorder.Code, recorder.Body.String())
	}

	remove := withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/esim/notifications/1", nil))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, remove)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"state":"removed"`) {
		t.Fatalf("notification remove = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/esim/notifications/not-a-number", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid sequence status = %d, want 400", recorder.Code)
	}
}

// confirmationCodeBackend 在 Download 时触发确认码请求，验证端到端交互链路。
type confirmationCodeBackend struct {
	*contractBackend
	requested chan struct{}
}

func (b *confirmationCodeBackend) Download(ctx context.Context, _ string, _ string, _ string, opts *backend.ESIMDownloadOptions) error {
	if opts == nil || opts.ConfirmationCodeRequest == nil {
		return fmt.Errorf("ConfirmationCodeRequest not wired")
	}
	close(b.requested)
	code, canceled, err := opts.ConfirmationCodeRequest()
	if err != nil {
		return err
	}
	if canceled {
		return fmt.Errorf("user declined")
	}
	if code != "2468" {
		return fmt.Errorf("code = %q, want 2468", code)
	}
	return nil
}

func TestEsimDownloadConfirmationCodeRoundTrip(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "fake-1", Product: "Fake modem"}}}
	b := &confirmationCodeBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		requested:       make(chan struct{}),
	}
	server, ops := newReadyServerWithBackend(t, discovery, b)
	handler := server.Handler()

	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"activation_code":"LPA:1$smdp.example.com$1-abc"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("download status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("download response = %s", recorder.Body.String())
	}

	select {
	case <-b.requested:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not request a confirmation code")
	}

	reply := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/operations/"+accepted.ID+"/confirmation-code", strings.NewReader(`{"code":"2468"}`)))
	reply.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, reply)
	if recorder.Code != http.StatusOK {
		t.Fatalf("confirmation reply status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, ok := ops.Get(accepted.ID)
		if ok && status.State == operation.Succeeded {
			return
		}
		if ok && status.State == operation.Failed {
			t.Fatalf("operation failed: %+v", status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download operation did not succeed after confirmation code reply")
}

func TestEsimNotificationHistoryEndpoint(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications/history", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		History []map[string]any `json:"history"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("history decode: %v", err)
	}
	if body.History == nil {
		t.Fatalf("history must be an array, got %s", recorder.Body.String())
	}
}

func TestSimCardsAPI(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	// 空列表。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/simcards", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cards":[]`) {
		t.Fatalf("empty list = %d %s", recorder.Code, recorder.Body.String())
	}

	// 手动建档。
	create := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/simcards",
		strings.NewReader(`{"iccid":"89860120010000000001","msisdn":"+8613800000000","name":"work","notes":"travel card"}`)))
	create.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, create)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	// 重复建档 → 409。
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/simcards",
		strings.NewReader(`{"iccid":"89860120010000000001"}`))))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", recorder.Code)
	}

	// 列表包含档案。
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/simcards", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "89860120010000000001") {
		t.Fatalf("list = %d %s", recorder.Code, recorder.Body.String())
	}

	// 更新元数据。
	update := withSameOrigin(httptest.NewRequest(http.MethodPut, "/api/v1/simcards/89860120010000000001",
		strings.NewReader(`{"name":"renamed","notes":"updated","msisdn":""}`)))
	update.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, update)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/simcards", nil))
	if !strings.Contains(recorder.Body.String(), "renamed") || !strings.Contains(recorder.Body.String(), "+8613800000000") {
		t.Fatalf("update result = %s", recorder.Body.String())
	}

	// 删除。
	remove := withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/simcards/89860120010000000001", nil))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, remove)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/simcards", nil))
	if !strings.Contains(recorder.Body.String(), `"cards":[]`) {
		t.Fatalf("list after delete = %s", recorder.Body.String())
	}

	// 删除不存在的卡 → 404。
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/simcards/89860120010000000002", nil)))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404", recorder.Code)
	}
}

func TestEsimConfirmationCodeReplyRejectsUnknownOperation(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()

	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/operations/no-such-op/confirmation-code", strings.NewReader(`{"code":"2468"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown operation status = %d, want 404", recorder.Code)
	}
}

func TestNotificationDebugAPI(t *testing.T) {
	server := newTestServer(t, nil)
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/debug", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "call_incoming") {
		t.Fatalf("debug capabilities = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/notifications/debug", strings.NewReader(`{"action":"call_incoming","call_id":"debug-1","number":"10010"}`)))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"type":"call.incoming"`) {
		t.Fatalf("debug publish = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/notifications/debug", strings.NewReader(`{"action":"invalid"}`)))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "unsupported notification debug action") {
		t.Fatalf("invalid debug action = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestNotificationPreferencesAndPermissionAPI(t *testing.T) {
	server := newTestServer(t, nil)
	preferences := notification.DefaultNotificationPreferences()
	permissionState := notification.NotificationPermissionDenied
	requestCalled, settingsCalled := 0, 0
	server.config.NotificationUIAvailable = func() bool { return true }
	server.config.NotificationPermissionStatus = func() notification.NotificationPermissionStatus {
		return notification.NotificationPermissionStatus{State: permissionState}
	}
	server.config.RequestNotificationPermission = func() bool { requestCalled++; return true }
	server.config.OpenNotificationSettings = func() bool { settingsCalled++; return true }
	server.config.NotificationPreferences = func() notification.NotificationPreferences { return preferences }
	server.config.SetNotificationPreferences = func(value notification.NotificationPreferences) error {
		preferences = value
		return nil
	}
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/preferences", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"incoming_call":"system"`) {
		t.Fatalf("preferences get = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(http.MethodPut, "/api/v1/notifications/preferences", strings.NewReader(`{"incoming_call":"custom","missed_call":"system","sms":"custom","device_offline":"system","show_debug":false}`)))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || preferences.IncomingCall != notification.NotificationPresentationCustom || preferences.SMS != notification.NotificationPresentationCustom || preferences.ShowDebug {
		t.Fatalf("preferences put = %d %s, value = %+v", recorder.Code, recorder.Body.String(), preferences)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/permissions", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"can_open_settings":true`) {
		t.Fatalf("permission get = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/notifications/permissions/open-settings", nil)))
	if recorder.Code != http.StatusAccepted || settingsCalled != 1 {
		t.Fatalf("permission settings = %d calls=%d %s", recorder.Code, settingsCalled, recorder.Body.String())
	}

	permissionState = notification.NotificationPermissionNotDetermined
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/notifications/permissions/request", nil)))
	if recorder.Code != http.StatusAccepted || requestCalled != 1 {
		t.Fatalf("permission request = %d calls=%d %s", recorder.Code, requestCalled, recorder.Body.String())
	}
}

func TestAPIContractCoversVoWiFiStatus(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "fake-vowifi"}}}
	caps := domain.CapabilitySet{
		domain.CapabilityDeviceStatus:  "",
		domain.CapabilityVoWiFiInspect: "",
		domain.CapabilityVoWiFiControl: "",
	}
	server, _ := newReadyServerWithBackend(t, discovery, &contractVoWiFiBackend{contractBackend: &contractBackend{caps: caps}})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/vowifi", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "disabled") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAPIContractReturnsStructuredOfflineAndUnsupportedErrors(t *testing.T) {
	server := newTestServer(t, nil)
	handler := server.Handler()
	checks := []struct {
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{method: http.MethodPost, path: "/api/v1/device/actions/raw-at", body: `{"command":"AT"}`, status: http.StatusServiceUnavailable, code: "device_offline"},
		{method: http.MethodGet, path: "/api/v1/network", status: http.StatusServiceUnavailable, code: "device_offline"},
	}
	for _, check := range checks {
		t.Run(check.path, func(t *testing.T) {
			var body io.Reader
			if check.body != "" {
				body = strings.NewReader(check.body)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(check.method, check.path, body)))
			if recorder.Code != check.status {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var envelope struct {
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != check.code || envelope.Error.Message == "" {
				t.Fatalf("structured error = %s", recorder.Body.String())
			}
		})
	}

	ready, _ := newReadyServer(t, domain.CapabilitySet{domain.CapabilityDeviceStatus: ""})
	recorder := httptest.NewRecorder()
	ready.Handler().ServeHTTP(recorder, withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/raw-at", strings.NewReader(`{"command":"AT"}`))))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "raw_at") {
		t.Fatalf("unsupported response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAPIContractRejectsInvalidPayloadAndMethod(t *testing.T) {
	server := newTestServer(t, nil)
	handler := server.Handler()

	for _, request := range []*http.Request{
		withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/sms/actions/send", strings.NewReader(`{"to":"+100"}`))),
		withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"matching_id":"x"}`))),
		httptest.NewRequest(http.MethodGet, "/api/v1/device/status", nil),
	} {
		if request.Method == http.MethodGet {
			request.Method = http.MethodPost
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("request %s %s returned %d: %s", request.Method, request.URL.Path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestAPIContractRejectsTrailingJSON(t *testing.T) {
	server := newTestServer(t, nil)
	recorder := httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/raw-at", strings.NewReader(`{"command":"AT"} {"extra":true}`)))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid JSON") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestPublicErrorMessageDoesNotExposeLocalizedBackendText(t *testing.T) {
	value := toStructuredError(derrors.New(derrors.Internal, "底层设备操作失败", true, nil))
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "底层设备操作失败") {
		t.Fatalf("localized backend text leaked into API error: %s", encoded)
	}
	if value.Message != derrors.PublicMessage(derrors.Internal) {
		t.Fatalf("public message = %q", value.Message)
	}
}

func TestAPIContractOperationStatusIsStable(t *testing.T) {
	server, ops := newReadyServer(t, allContractCapabilities())
	var startErr error
	id, startErr := ops.Start(context.Background(), "test", func(context.Context, string, func(int, string)) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	if startErr != nil {
		t.Fatalf("start: %v", startErr)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/operations/"+id, nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), id) {
		t.Fatalf("operation status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

const domainErrorUnauthenticated = "unauthenticated"

type brokenSIMBackend struct{ *contractBackend }

func (b *brokenSIMBackend) SIM(context.Context) (backend.SIMState, error) {
	return backend.SIMState{}, derrors.New(derrors.Internal, "设备返回错误: +CME ERROR: 10", true, nil)
}

// TestDeviceStatusSurvivesSubQueryFailure: 单项能力查询失败(如无 SIM 时 AT 返回
// +CME ERROR: 10)时,/device/status 仍返回 200 并携带已检测到的设备身份,而不是
// 500 让前端误以为没有兼容的模组。
func TestDeviceStatusSurvivesSubQueryFailure(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "fake-1", Product: "Quectel 4G Module"}}}
	b := &brokenSIMBackend{contractBackend: &contractBackend{caps: allContractCapabilities()}}
	server, _ := newReadyServerWithBackend(t, discovery, b)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/device/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Snapshot struct {
			State    string `json:"state"`
			Identity struct {
				StableID string `json:"stable_id"`
				Product  string `json:"product"`
			} `json:"identity"`
			LastError string `json:"last_error"`
		} `json:"snapshot"`
		SIM struct {
			Inserted bool `json:"inserted"`
		} `json:"sim"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Snapshot.State != string(domain.StateReady) {
		t.Fatalf("state = %q", body.Snapshot.State)
	}
	if body.Snapshot.Identity.StableID != "fake-1" || body.Snapshot.Identity.Product != "Quectel 4G Module" {
		t.Fatalf("identity = %#v", body.Snapshot.Identity)
	}
	if body.Snapshot.LastError == "" {
		t.Fatal("expected last_error to carry the SIM failure")
	}
	if body.SIM.Inserted {
		t.Fatal("sim should not be marked inserted")
	}
}

type oneCandidateDiscovery struct{}

func (oneCandidateDiscovery) Discover(context.Context) ([]domain.Candidate, error) {
	return []domain.Candidate{{Identity: domain.Identity{StableID: "test/1"}}}, nil
}

type failingFactory struct{}

func (failingFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return nil, "", derrors.New(derrors.Internal, "probe failed", true, nil)
}

// TestWebSocketStaysOpenWhenSnapshotFails drives the runtime into the degraded
// state (backend init failed, e.g. the modem answering CME ERROR), where the
// initial snapshot query errors. The websocket must remain open and keep
// delivering runtime events instead of closing right after the upgrade.
func TestWebSocketStaysOpenWhenSnapshotFails(t *testing.T) {
	r, err := runtime.New(runtime.Config{
		Discovery: oneCandidateDiscovery{}, Backends: failingFactory{},
		PollInterval: time.Hour, // keep background rescans out of the test
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err == nil {
		t.Fatal("expected rescan to fail backend probing")
	}
	if got := r.Snapshot().State; got != domain.StateDegraded {
		t.Fatalf("state = %s, want %s", got, domain.StateDegraded)
	}
	server := newTestServerWithRuntime(t, nil, r)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(ts.URL, "http://")+"/api/v1/events/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The server subscribes after the upgrade; publish repeatedly until the
	// event is observed so the test is not sensitive to that ordering.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.Events().Publish("device.status.changed", map[string]any{"state": "degraded"})
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, payload, err := conn.ReadMessage()
		if err == nil {
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Fatalf("bad envelope: %v", err)
			}
			if envelope.Type != "device.status.changed" {
				t.Fatalf("envelope type = %q", envelope.Type)
			}
			return
		}
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("websocket closed instead of staying open: %v", err)
		}
	}
	t.Fatal("no event delivered within deadline")
}

// settingsJSONStore is a minimal storage.ValueStore keeping one JSON document.
type settingsJSONStore struct{ value string }

func (s *settingsJSONStore) Read(value any) error {
	if s.value == "" {
		return nil
	}
	return json.Unmarshal([]byte(s.value), value)
}

func (s *settingsJSONStore) Write(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.value = string(encoded)
	return nil
}

func TestFirmwareADBSettingsAPI(t *testing.T) {
	r, err := runtime.New(runtime.Config{Discovery: emptyDiscovery{}, Backends: emptyFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	ops := operation.NewManager(r.Events())
	store := &settingsJSONStore{}
	firmwareService := firmware.NewService(nil, ops, r, firmware.Config{Store: store})
	server := NewServer(Config{
		Firmware: firmwareService, Operations: ops, Runtime: r,
		Auth: AuthenticatorFunc(func(*http.Request) bool { return true }), LoopbackPort: testLoopbackPort,
	})
	handler := server.Handler()

	executable := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/firmware/actions/adb/settings", strings.NewReader(fmt.Sprintf(`{"command":%q}`, executable))))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["command"] != executable || result["command_source"] != "saved" {
		t.Fatalf("response = %#v", result)
	}

	// A fresh service reads the persisted value back from the store.
	reloaded := firmware.NewService(nil, ops, r, firmware.Config{Store: store})
	if command, source := reloaded.ADBCommandConfig(); command != executable || source != "saved" {
		t.Fatalf("persisted command = %q, %q; want %q, saved", command, source, executable)
	}

	// A command that does not resolve to an executable is rejected.
	recorder = httptest.NewRecorder()
	request = withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/firmware/actions/adb/settings", strings.NewReader(`{"command":"definitely-not-a-real-command-xyz"}`)))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid command status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}
