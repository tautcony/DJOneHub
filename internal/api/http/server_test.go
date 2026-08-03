package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
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

func newTestServer(t *testing.T, auth Authenticator) *Server {
	t.Helper()
	r, err := runtime.New(runtime.Config{Discovery: emptyDiscovery{}, Backends: emptyFactory{}})
	if err != nil {
		t.Fatal(err)
	}
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
		Operations: ops, Runtime: r, Auth: auth,
	})
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/rescan", nil)
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

func (b *contractBackend) ReadSMS(context.Context, int) (backend.SMSMessage, error) {
	return backend.SMSMessage{Index: 1, Sender: "+100", Body: "hello"}, nil
}
func (b *contractBackend) DeleteSMS(context.Context, int) error { return nil }
func (b *contractBackend) DeleteAllSMS(context.Context) error   { return nil }
func (b *contractBackend) EID(context.Context) (string, error) {
	return "89049032000000000000000000000000", nil
}
func (b *contractBackend) Profiles(context.Context) ([]backend.Profile, error) {
	return []backend.Profile{{ICCID: "8901000000000000000", State: "active"}}, nil
}
func (b *contractBackend) Download(context.Context, string, string, string) error { return nil }
func (b *contractBackend) Enable(context.Context, string) error                   { return nil }
func (b *contractBackend) Rename(context.Context, string, string) error           { return nil }
func (b *contractBackend) Delete(context.Context, string) error                   { return nil }
func (b *contractBackend) RawAT(context.Context, string) (string, error)          { return "OK", nil }

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
	networkService := network.NewService(devices, ops, r, contractNetwork{})
	rawATService := rawat.NewService(devices, r)
	vowifiService := vowifi.NewService(devices, ops, r)
	notificationService := notification.New(notification.Config{Events: r.Events()})
	return NewServer(Config{
		Device: devices, SMS: smsService, ESIM: esimService, Network: networkService,
		Notification: notificationService, RawAT: rawATService, VoWiFi: vowifiService,
		Operations: ops, Runtime: r,
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
		{name: "sms", path: "/api/v1/sms"},
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sms/actions/send", strings.NewReader(`{"to":"+100","body":"hello"}`))
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

func TestNotificationDebugAPI(t *testing.T) {
	server := newTestServer(t, nil)
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/debug", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "call_incoming") || !strings.Contains(recorder.Body.String(), "gps_fix") {
		t.Fatalf("debug capabilities = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/debug", strings.NewReader(`{"action":"call_incoming","call_id":"debug-1","number":"10010"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"type":"call.incoming"`) {
		t.Fatalf("debug publish = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications/debug", strings.NewReader(`{"action":"invalid"}`))
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
	request := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/preferences", strings.NewReader(`{"incoming_call":"custom","missed_call":"system","sms":"custom","device_offline":"system"}`))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || preferences.IncomingCall != notification.NotificationPresentationCustom || preferences.SMS != notification.NotificationPresentationCustom {
		t.Fatalf("preferences put = %d %s, value = %+v", recorder.Code, recorder.Body.String(), preferences)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/permissions", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"can_open_settings":true`) {
		t.Fatalf("permission get = %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/permissions/open-settings", nil))
	if recorder.Code != http.StatusAccepted || settingsCalled != 1 {
		t.Fatalf("permission settings = %d calls=%d %s", recorder.Code, settingsCalled, recorder.Body.String())
	}

	permissionState = notification.NotificationPermissionNotDetermined
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/permissions/request", nil))
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
			handler.ServeHTTP(recorder, httptest.NewRequest(check.method, check.path, body))
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
	ready.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/raw-at", strings.NewReader(`{"command":"AT"}`)))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "raw_at") {
		t.Fatalf("unsupported response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAPIContractRejectsInvalidPayloadAndMethod(t *testing.T) {
	server := newTestServer(t, nil)
	handler := server.Handler()

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/sms/actions/send", strings.NewReader(`{"to":"+100"}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"matching_id":"x"}`)),
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/device/actions/raw-at", strings.NewReader(`{"command":"AT"} {"extra":true}`))
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
	id := ops.Start(context.Background(), "test", func(context.Context, func(int, string)) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/operations/"+id, nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), id) {
		t.Fatalf("operation status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

const domainErrorUnauthenticated = "unauthenticated"
