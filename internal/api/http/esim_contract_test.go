package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
)

type overviewContractBackend struct {
	*contractBackend
	eidErr   error
	profiles []backend.Profile
	radioErr error
}

type renameContractBackend struct {
	*contractBackend
	renameErr error
	lastICCID string
	lastLabel string
}

type notificationContractBackend struct {
	*contractBackend
	processErr error
	removeErr  error
}

type downloadContractBackend struct {
	*contractBackend
	done            chan struct{}
	requested       chan struct{}
	requestConfirm  bool
	err             error
	gotSMDP         string
	gotMatchingID   string
	gotConfirmation string
	progressSeen    []int
	declined        bool
}

func (b *downloadContractBackend) Download(_ context.Context, smdp, confirmationCode, matchingID string, opts *backend.ESIMDownloadOptions) error {
	b.gotSMDP, b.gotConfirmation, b.gotMatchingID = smdp, confirmationCode, matchingID
	if opts != nil && opts.Progress != nil {
		opts.Progress("auth_client", 42, "authenticated")
		b.progressSeen = append(b.progressSeen, 42)
	}
	if b.requestConfirm {
		if b.requested != nil {
			close(b.requested)
		}
		code, declined, err := opts.ConfirmationCodeRequest()
		if err != nil {
			return err
		}
		b.declined = declined
		if declined || code != "2468" {
			return errors.New("confirmation declined")
		}
	}
	if b.done != nil {
		close(b.done)
	}
	return b.err
}

func (b *notificationContractBackend) ListNotifications(context.Context) ([]backend.NotificationItem, error) {
	return []backend.NotificationItem{{
		SequenceNumber: 31,
		Event:          "install",
		ICCID:          "8901000000000000000",
		Address:        "smdp.example.com",
		CanRetry:       true,
	}}, nil
}

func (b *notificationContractBackend) ProcessNotification(context.Context, int64) error {
	return b.processErr
}

func (b *notificationContractBackend) RemoveNotification(context.Context, int64) error {
	return b.removeErr
}

func (b *renameContractBackend) Rename(_ context.Context, iccid, label string) error {
	b.lastICCID, b.lastLabel = iccid, label
	return b.renameErr
}

func (b *overviewContractBackend) EID(context.Context) (string, error) {
	if b.eidErr != nil {
		return "", b.eidErr
	}
	return "89049032000000000000000000000000", nil
}

func (b *overviewContractBackend) Profiles(context.Context) ([]backend.Profile, error) {
	return b.profiles, nil
}

func (b *overviewContractBackend) ESIMStorage(context.Context) (backend.ESIMStorageInfo, error) {
	return backend.ESIMStorageInfo{FreeNvramBytes: 220160, FreeNvram: "215.00 KB"}, nil
}

func (b *overviewContractBackend) ESIMDeviceInfo(context.Context) (backend.ESIMDeviceInfo, error) {
	return backend.ESIMDeviceInfo{
		SKU:          "ESTKme Light",
		SerialNumber: "3107110a-05534132",
		Firmware:     "T3VASS0-5.8.11.1",
	}, nil
}

func (b *overviewContractBackend) Radio(context.Context) (backend.RadioState, error) {
	if b.radioErr != nil {
		return backend.RadioState{}, b.radioErr
	}
	return b.contractBackend.Radio(context.Background())
}

func TestEsimOverviewContractStates(t *testing.T) {
	tests := []struct {
		name      string
		backend   *overviewContractBackend
		wantType  string
		wantEID   string
		wantCount int
		wantFree  string
	}{
		{
			name: "readable euicc with profiles",
			backend: &overviewContractBackend{
				contractBackend: &contractBackend{caps: allContractCapabilities()},
				profiles:        []backend.Profile{{ICCID: "8901000000000000000", State: "enabled"}},
			},
			wantType: "euicc", wantEID: "89049032000000000000000000000000", wantCount: 1, wantFree: "215.00 KB",
		},
		{
			name: "readable euicc with no profiles",
			backend: &overviewContractBackend{
				contractBackend: &contractBackend{caps: allContractCapabilities()},
				profiles:        []backend.Profile{},
			},
			wantType: "euicc", wantEID: "89049032000000000000000000000000", wantCount: 0, wantFree: "215.00 KB",
		},
		{
			name: "unreadable euicc",
			backend: &overviewContractBackend{
				contractBackend: &contractBackend{caps: allContractCapabilities()},
				eidErr:          errors.New("no eUICC found during AT+CCHO"),
			},
			wantType: "unknown", wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: tc.name}}}, tc.backend)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/esim", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("overview status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var body struct {
				CardType string                 `json:"card_type"`
				EID      string                 `json:"eid"`
				Free     string                 `json:"free_nvram"`
				Device   backend.ESIMDeviceInfo `json:"device_info"`
				Profiles []backend.Profile      `json:"profiles"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.CardType != tc.wantType || body.EID != tc.wantEID || len(body.Profiles) != tc.wantCount {
				t.Fatalf("overview = %#v, want type=%q eid=%q profiles=%d", body, tc.wantType, tc.wantEID, tc.wantCount)
			}
			if body.Free != tc.wantFree {
				t.Fatalf("free_nvram = %q, want %q", body.Free, tc.wantFree)
			}
			if tc.wantType == "euicc" && (body.Device.SKU != "ESTKme Light" || body.Device.Firmware != "T3VASS0-5.8.11.1") {
				t.Fatalf("device_info = %#v", body.Device)
			}
			registered, err := server.config.SimProfiles.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(registered) != tc.wantCount {
				t.Fatalf("registered profiles = %+v, want %d", registered, tc.wantCount)
			}
		})
	}
}

func TestEsimHealthContractHasStableFields(t *testing.T) {
	backend := &overviewContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		profiles:        []backend.Profile{{ICCID: "8901000000000000000", State: "enabled"}},
	}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "health"}}}, backend)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/esim/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ok", "module_iccid", "operator", "registration", "registered", "network_mode", "active_profile"} {
		if _, ok := body[field]; !ok {
			t.Fatalf("health field %q missing from %s", field, recorder.Body.String())
		}
	}
}

func TestEsimCardOperationsReturnIDsAndConverge(t *testing.T) {
	server, ops := newReadyServer(t, allContractCapabilities())
	handler := server.Handler()
	for _, action := range []string{"enable", "disable", "delete"} {
		t.Run(action, func(t *testing.T) {
			request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/"+action,
				strings.NewReader(`{"iccid":"8901000000000000000"}`)))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var accepted struct {
				ID string `json:"operation_id"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
				t.Fatalf("response = %s", recorder.Body.String())
			}
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				status, ok := ops.Get(accepted.ID)
				if ok && status.State == operation.Succeeded {
					return
				}
				if ok && status.State == operation.Failed {
					t.Fatalf("operation failed: %+v", status.Error)
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatalf("operation %s did not converge", accepted.ID)
		})
	}
}

func TestEsimDeleteRejectsBootstrapProfile(t *testing.T) {
	b := &overviewContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		profiles: []backend.Profile{{
			ICCID:               "8901000000000000000",
			State:               "disabled",
			Label:               "Bootstrap",
			ServiceProviderName: "Bootstrap",
			ProfileClass:        "operational",
		}},
	}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "bootstrap-delete"}}}, b)
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/delete",
		strings.NewReader(`{"iccid":"8901000000000000000"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("bootstrap delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestEsimNotificationHistoryTracksProcessAndRemoveOutcomes(t *testing.T) {
	backend := &notificationContractBackend{contractBackend: &contractBackend{caps: allContractCapabilities()}}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "notification-history"}}}, backend)
	handler := server.Handler()

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list notifications = %d %s", list.Code, list.Body.String())
	}

	process := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/notifications/31/process", strings.NewReader(`{}`)))
	process.Header.Set("Content-Type", "application/json")
	processed := httptest.NewRecorder()
	handler.ServeHTTP(processed, process)
	if processed.Code != http.StatusOK {
		t.Fatalf("process notification = %d %s", processed.Code, processed.Body.String())
	}
	assertNotificationHistoryState(t, handler, "processed")

	remove := withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/esim/notifications/31", nil))
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, remove)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove notification = %d %s", removed.Code, removed.Body.String())
	}
	assertNotificationHistoryState(t, handler, "removed")
}

func TestEsimNotificationProcessFailureIsRecordedInHistory(t *testing.T) {
	backend := &notificationContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		processErr:      errors.New("notification retry failed"),
	}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "notification-failure"}}}, backend)
	handler := server.Handler()

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list notifications = %d %s", list.Code, list.Body.String())
	}

	process := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/notifications/31/process", strings.NewReader(`{}`)))
	process.Header.Set("Content-Type", "application/json")
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, process)
	if failed.Code == http.StatusOK {
		t.Fatalf("process failure was reported as success: %s", failed.Body.String())
	}
	assertNotificationHistoryState(t, handler, "failed")
}

func TestEsimNotificationRemoveFailureIsRecordedInHistory(t *testing.T) {
	backend := &notificationContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		removeErr:       errors.New("notification removal failed"),
	}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "notification-remove-failure"}}}, backend)
	handler := server.Handler()

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list notifications = %d %s", list.Code, list.Body.String())
	}

	remove := withSameOrigin(httptest.NewRequest(http.MethodDelete, "/api/v1/esim/notifications/31", nil))
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, remove)
	if failed.Code == http.StatusOK {
		t.Fatalf("remove failure was reported as success: %s", failed.Body.String())
	}
	assertNotificationHistoryState(t, handler, "failed")
}

func TestEsimDownloadResolvesActivationCodeForwardsProgressAndCleansUp(t *testing.T) {
	backend := &downloadContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		done:            make(chan struct{}),
	}
	server, ops := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "download-contract"}}}, backend)
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"activation_code":"LPA:1$smdp.example.com$embedded","confirmation_code":"initial","matching_id":"typed"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("download = %d %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("accepted download = %s", recorder.Body.String())
	}
	select {
	case <-backend.done:
	case <-time.After(2 * time.Second):
		t.Fatal("download backend did not finish")
	}
	if backend.gotSMDP != "smdp.example.com" || backend.gotMatchingID != "embedded" || backend.gotConfirmation != "initial" {
		t.Fatalf("download args = smdp=%q matching=%q confirmation=%q", backend.gotSMDP, backend.gotMatchingID, backend.gotConfirmation)
	}
	if len(backend.progressSeen) != 1 || backend.progressSeen[0] != 42 {
		t.Fatalf("progress = %v, want staged 42", backend.progressSeen)
	}
	status, ok := waitForOperation(t, ops, accepted.ID)
	if !ok || status.State != operation.Succeeded {
		t.Fatalf("download status = %+v, want succeeded", status)
	}
}

func TestEsimDownloadConfirmationDeclineFailsAndRemovesPendingRequest(t *testing.T) {
	backend := &downloadContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		done:            make(chan struct{}),
		requested:       make(chan struct{}),
		requestConfirm:  true,
	}
	server, ops := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "download-decline"}}}, backend)
	handler := server.Handler()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"activation_code":"smdp.example.com"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("download = %d %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("accepted download = %s", recorder.Body.String())
	}
	select {
	case <-backend.requested:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not request confirmation")
	}

	reply := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/operations/"+accepted.ID+"/confirmation-code", strings.NewReader(`{"declined":true}`)))
	reply.Header.Set("Content-Type", "application/json")
	replyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replyRecorder, reply)
	if replyRecorder.Code != http.StatusOK {
		t.Fatalf("decline = %d %s", replyRecorder.Code, replyRecorder.Body.String())
	}
	status, ok := waitForOperation(t, ops, accepted.ID)
	if !ok || status.State != operation.Failed || !backend.declined {
		t.Fatalf("declined download status = %+v declined=%v", status, backend.declined)
	}
	late := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/operations/"+accepted.ID+"/confirmation-code", strings.NewReader(`{"code":"2468"}`)))
	late.Header.Set("Content-Type", "application/json")
	lateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(lateRecorder, late)
	if lateRecorder.Code != http.StatusNotFound {
		t.Fatalf("late confirmation = %d %s, want 404 after cleanup", lateRecorder.Code, lateRecorder.Body.String())
	}
}

func TestEsimDownloadBackendFailureReachesTerminalState(t *testing.T) {
	backend := &downloadContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		done:            make(chan struct{}),
		err:             errors.New("SM-DP+ unavailable"),
	}
	server, ops := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "download-failure"}}}, backend)
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"activation_code":"smdp.example.com"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("download = %d %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("accepted download = %s", recorder.Body.String())
	}
	status, ok := waitForOperation(t, ops, accepted.ID)
	if !ok || status.State != operation.Failed || status.Error == nil {
		t.Fatalf("failed download status = %+v", status)
	}
}

func TestEsimDownloadCancellationWhileAwaitingConfirmationCleansUp(t *testing.T) {
	backend := &downloadContractBackend{
		contractBackend: &contractBackend{caps: allContractCapabilities()},
		requested:       make(chan struct{}),
		requestConfirm:  true,
	}
	server, ops := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "download-cancel"}}}, backend)
	handler := server.Handler()
	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/download", strings.NewReader(`{"activation_code":"smdp.example.com"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("download = %d %s", recorder.Code, recorder.Body.String())
	}
	var accepted struct {
		ID string `json:"operation_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil || accepted.ID == "" {
		t.Fatalf("accepted download = %s", recorder.Body.String())
	}
	select {
	case <-backend.requested:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not request confirmation")
	}
	if !ops.Cancel(accepted.ID) {
		t.Fatal("active download was not cancellable")
	}
	status, ok := waitForOperation(t, ops, accepted.ID)
	if !ok || status.State != operation.Cancelled {
		t.Fatalf("cancelled download status = %+v", status)
	}
	late := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/operations/"+accepted.ID+"/confirmation-code", strings.NewReader(`{"code":"2468"}`)))
	late.Header.Set("Content-Type", "application/json")
	lateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(lateRecorder, late)
	if lateRecorder.Code != http.StatusNotFound {
		t.Fatalf("late confirmation = %d %s, want 404 after cancellation cleanup", lateRecorder.Code, lateRecorder.Body.String())
	}
}

func waitForOperation(t *testing.T, ops *operation.Manager, id string) (operation.Status, bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, ok := ops.Get(id)
		if ok && (status.State == operation.Succeeded || status.State == operation.Failed || status.State == operation.Cancelled) {
			return status, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, ok := ops.Get(id)
	return status, ok
}

func assertNotificationHistoryState(t *testing.T, handler http.Handler, want string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/esim/notifications/history", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("history = %d %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		History []struct {
			State string `json:"state"`
			Event string `json:"event"`
		} `json:"history"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.History) != 1 || body.History[0].State != want || body.History[0].Event != "install" {
		t.Fatalf("history = %#v, want install/%s", body.History, want)
	}
}

func TestEsimRenameContractValidationAndFailure(t *testing.T) {
	b := &renameContractBackend{contractBackend: &contractBackend{caps: allContractCapabilities()}}
	server, _ := newReadyServerWithBackend(t, &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "rename"}}}, b)
	handler := server.Handler()

	request := withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/rename",
		strings.NewReader(`{"iccid":"8901000000000000000","label":"Travel"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || b.lastICCID != "8901000000000000000" || b.lastLabel != "Travel" {
		t.Fatalf("rename success = %d %s, target=%q label=%q", recorder.Code, recorder.Body.String(), b.lastICCID, b.lastLabel)
	}

	request = withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/rename",
		strings.NewReader(`{"iccid":"8901000000000000000","label":""}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty label status = %d, want 400", recorder.Code)
	}

	b.renameErr = errors.New("rename rejected")
	request = withSameOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/esim/actions/rename",
		strings.NewReader(`{"iccid":"8901000000000000000","label":"Blocked"}`)))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("backend rename failure was reported as success: %s", recorder.Body.String())
	}
}
