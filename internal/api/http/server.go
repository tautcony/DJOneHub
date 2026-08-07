package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	nethttp "net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/extras"
	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/simcards"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/backend"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/platform/startup"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
)

type Authenticator interface{ Authenticate(*nethttp.Request) bool }
type AuthenticatorFunc func(*nethttp.Request) bool

func (f AuthenticatorFunc) Authenticate(r *nethttp.Request) bool { return f(r) }

type Config struct {
	Device                        *device.Service
	SMS                           *sms.Service
	ESIM                          *esim.Service
	SimCards                      *simcards.Service
	Network                       *network.Service
	Notification                  *notification.Service
	RawAT                         *rawat.Service
	VoWiFi                        *vowifi.Service
	Extras                        *extras.Service
	Firmware                      *firmware.Service
	Operations                    *operation.Manager
	Runtime                       *runtime.Runtime
	Auth                          Authenticator
	NotificationUIAvailable       func() bool
	NotificationPermissionStatus  func() notification.NotificationPermissionStatus
	RequestNotificationPermission func() bool
	OpenNotificationSettings      func() bool
	NotificationPreferences       func() notification.NotificationPreferences
	SetNotificationPreferences    func(notification.NotificationPreferences) error
	StartupStatus                 func() startup.Status
	SetStartupEnabled             func(bool) error
	// LoopbackPort is the port the server binds on loopback. It anchors the
	// temporary boundary's Origin/Host checks; set via SetLoopbackPort before
	// serving. The guard fails closed when it is unset.
	LoopbackPort int
	// Admission reports whether the application still admits new requests. It
	// is closed before the HTTP server drains, so requests that arrive during
	// shutdown are refused instead of starting new work. nil admits everything.
	Admission func() bool
}

type Server struct {
	config Config
	// keepalive is captured at construction so tests can shrink the WebSocket
	// windows per server without racing handler goroutines.
	keepalive websocketKeepalive
}

func NewServer(config Config) *Server {
	return &Server{config: config, keepalive: websocketKeepalive{write: writeWait, pong: pongWait, ping: pingPeriod}}
}

// SetLoopbackPort records the bound loopback port used to validate Origin and
// Host metadata on state-changing requests and WebSocket upgrades. It must be
// called before serving; without it the boundary rejects every state-changing
// request and upgrade.
func (s *Server) SetLoopbackPort(port int) { s.config.LoopbackPort = port }

func (s *Server) Handler() nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/api/v1/device", s.deviceStatus)
	mux.HandleFunc("/api/v1/device/capabilities", s.deviceCapabilities)
	mux.HandleFunc("/api/v1/device/status", s.deviceStatus)
	mux.HandleFunc("/api/v1/device/actions/rescan", s.rescan)
	mux.HandleFunc("/api/v1/device/actions/reboot", s.reboot)
	mux.HandleFunc("/api/v1/sms/actions/refresh", s.smsRefresh)
	mux.HandleFunc("/api/v1/sms/actions/send", s.smsSend)
	mux.HandleFunc("/api/v1/sms/actions/clear", s.smsClear)
	mux.HandleFunc("/api/v1/esim", s.esimOverview)
	mux.HandleFunc("/api/v1/esim/actions/download", s.esimDownload)
	mux.HandleFunc("/api/v1/esim/actions/enable", s.esimEnable)
	mux.HandleFunc("/api/v1/esim/actions/rename", s.esimRename)
	mux.HandleFunc("/api/v1/esim/actions/delete", s.esimDelete)
	mux.HandleFunc("/api/v1/esim/actions/disable", s.esimDisable)
	mux.HandleFunc("/api/v1/esim/notifications", s.esimNotifications)
	mux.HandleFunc("/api/v1/esim/notifications/", s.esimNotificationBySequence)
	mux.HandleFunc("/api/v1/esim/operations/", s.esimConfirmationCodeReply)
	mux.HandleFunc("/api/v1/network", s.networkStatus)
	mux.HandleFunc("/api/v1/network/actions/mode", s.networkMode)
	mux.HandleFunc("/api/v1/network/actions/check", s.networkCheck)
	mux.HandleFunc("/api/v1/network/actions/traffic", s.networkTraffic)
	mux.HandleFunc("/api/v1/network/traffic/daily", s.networkTrafficDaily)
	mux.HandleFunc("/api/v1/network/traffic/range", s.networkTrafficRange)
	mux.HandleFunc("/api/v1/network/diagnostics", s.networkDiagnostics)
	mux.HandleFunc("/api/v1/device/actions/raw-at", s.rawAT)
	mux.HandleFunc("/api/v1/firmware", s.firmwareStatus)
	mux.HandleFunc("/api/v1/firmware/actions/adb/unlock", s.firmwareADBUnlock)
	mux.HandleFunc("/api/v1/firmware/actions/adb/mode", s.firmwareADBMode)
	mux.HandleFunc("/api/v1/firmware/actions/adb/shell/ws", s.firmwareADBShellWS)
	mux.HandleFunc("/api/v1/firmware/actions/usb-id", s.firmwareUSBID)
	mux.HandleFunc("/api/v1/firmware/actions/mode", s.firmwareMode)
	mux.HandleFunc("/api/v1/firmware/actions/backup", s.firmwareBackup)
	mux.HandleFunc("/api/v1/firmware/actions/backup/select-directory", s.firmwareBackupSelectDirectory)
	mux.HandleFunc("/api/v1/firmware/actions/backup/select-edl-directory", s.firmwareBackupSelectEDLDirectory)
	mux.HandleFunc("/api/v1/firmware/actions/select-adb-file", s.firmwareSelectADBFile)
	mux.HandleFunc("/api/v1/firmware/actions/adb/settings", s.firmwareSetADBSettings)
	mux.HandleFunc("/api/v1/vowifi", s.vowifiStatus)
	mux.HandleFunc("/api/v1/vowifi/actions/enable", s.vowifiEnable)
	mux.HandleFunc("/api/v1/vowifi/actions/disable", s.vowifiDisable)
	mux.HandleFunc("/api/v1/vowifi/actions/reconnect", s.vowifiReconnect)
	mux.HandleFunc("/api/v1/notifications/debug", s.notificationDebug)
	mux.HandleFunc("/api/v1/notifications/permissions", s.notificationPermissions)
	mux.HandleFunc("/api/v1/notifications/permissions/request", s.requestNotificationPermission)
	mux.HandleFunc("/api/v1/notifications/permissions/open-settings", s.openNotificationSettings)
	mux.HandleFunc("/api/v1/notifications/preferences", s.notificationPreferences)
	mux.HandleFunc("/api/v1/settings/startup", s.startupSettings)
	mux.HandleFunc("/api/v1/calls", s.calls)
	mux.HandleFunc("/api/v1/calls/actions/dial", s.callDial)
	mux.HandleFunc("/api/v1/calls/actions/reject", s.callReject)
	mux.HandleFunc("/api/v1/esim/health", s.esimHealth)
	mux.HandleFunc("/api/v1/esim/notes", s.esimNotes)
	mux.HandleFunc("/api/v1/simcards", s.simCards)
	mux.HandleFunc("/api/v1/simcards/", s.simCardByICCID)
	mux.HandleFunc("/api/v1/operations/", s.operationStatus)
	mux.HandleFunc("/api/v1/openapi.json", s.openapi)
	mux.HandleFunc("/api/v1/events/ws", s.websocket)
	return logRequests(admissionGate(s.config.Admission, s.loopbackGuard(mux)))
}

// admissionGate refuses new requests once the shutdown admission gate is
// closed, so an already-draining server cannot start new work.
func admissionGate(admit func() bool, next nethttp.Handler) nethttp.Handler {
	if admit == nil {
		return next
	}
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !admit() {
			writeError(w, derrors.New(derrors.Unavailable, "the application is shutting down", false, nil))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) protected(w nethttp.ResponseWriter, r *nethttp.Request) bool {
	if s.config.Auth == nil || s.config.Auth.Authenticate(r) {
		return true
	}
	writeError(w, derrors.New(derrors.Unauthenticated, "local authentication required", false, nil))
	return false
}

func (s *Server) deviceStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	status, err := s.config.Device.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, sanitizeDeviceStatus(status))
}

func (s *Server) deviceCapabilities(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	snapshot := sanitizeSnapshot(s.config.Runtime.Snapshot())
	writeJSON(w, nethttp.StatusOK, map[string]any{
		"backend":        snapshot.Backend,
		"backend_reason": snapshot.BackendReason,
		"capabilities":   snapshot.Capabilities,
	})
}

func (s *Server) rescan(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	if err := s.config.Device.Rescan(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"state": sanitizeSnapshot(s.config.Runtime.Snapshot())})
}

func (s *Server) reboot(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if err := s.config.Device.Reboot(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]any{"accepted": true})
}

func (s *Server) smsRefresh(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	items, err := s.config.SMS.Refresh(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

type sendSMSRequest struct {
	To   string `json:"to"`
	Body string `json:"body"`
}

func (s *Server) smsSend(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	var request sendSMSRequest
	if err := decodeJSON(r, &request); err != nil || strings.TrimSpace(request.To) == "" || request.Body == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "to and body are required", false, nil))
		return
	}
	id, err := s.config.SMS.Send(r.Context(), request.To, request.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) smsClear(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	if err := s.config.SMS.Clear(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"state": "cleared"})
}

type esimRequest struct {
	ActivationCode   string `json:"activation_code"`
	ConfirmationCode string `json:"confirmation_code"`
	MatchingID       string `json:"matching_id"`
	ICCID            string `json:"iccid"`
	Label            string `json:"label"`
}

func (s *Server) esimOverview(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.ESIM.Overview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, publicESIMOverview(value))
}

func (s *Server) esimDownload(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value esimRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.ActivationCode) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "activation_code is required", false, nil))
		return
	}
	id, err := s.config.ESIM.Download(r.Context(), value.ActivationCode, value.ConfirmationCode, value.MatchingID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) esimEnable(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value esimRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.ICCID) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "iccid is required", false, nil))
		return
	}
	id, err := s.config.ESIM.Enable(r.Context(), value.ICCID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) esimRename(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value esimRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.ICCID) == "" || strings.TrimSpace(value.Label) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "iccid and label are required", false, nil))
		return
	}
	if err := s.config.ESIM.Rename(r.Context(), value.ICCID, value.Label); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"state": "renamed"})
}

func (s *Server) esimDelete(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value esimRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.ICCID) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "iccid is required", false, nil))
		return
	}
	id, err := s.config.ESIM.Delete(r.Context(), value.ICCID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) esimDisable(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value esimRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.ICCID) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "iccid is required", false, nil))
		return
	}
	id, err := s.config.ESIM.Disable(r.Context(), value.ICCID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) esimNotifications(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	items, err := s.config.ESIM.ListNotifications(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"notifications": items})
}

func (s *Server) esimNotificationBySequence(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/esim/notifications/")
	if rest == "history" {
		if !s.requireMethod(w, r, nethttp.MethodGet) {
			return
		}
		records, err := s.config.ESIM.NotificationHistory(r.Context(), 0)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"history": publicNotificationHistory(records)})
		return
	}
	parts := strings.Split(rest, "/")
	if strings.TrimSpace(parts[0]) == "" {
		writeError(w, derrors.New(derrors.NotFound, "notification not found", false, nil))
		return
	}
	sequenceNumber, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, derrors.New(derrors.InvalidRequest, "invalid notification sequence number", false, nil))
		return
	}
	switch {
	case r.Method == nethttp.MethodPost && len(parts) == 2 && parts[1] == "process":
		if err := s.config.ESIM.ProcessNotification(r.Context(), sequenceNumber); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]string{"state": "processed"})
	case r.Method == nethttp.MethodDelete && len(parts) == 1:
		if err := s.config.ESIM.RemoveNotification(r.Context(), sequenceNumber); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]string{"state": "removed"})
	default:
		writeError(w, derrors.New(derrors.NotFound, "notification not found", false, nil))
	}
}

type simCardRequest struct {
	ICCID  string `json:"iccid"`
	IMSI   string `json:"imsi"`
	MSISDN string `json:"msisdn"`
	Name   string `json:"name"`
	Notes  string `json:"notes"`
}

func (s *Server) simCards(w nethttp.ResponseWriter, r *nethttp.Request) {
	switch {
	case r.Method == nethttp.MethodGet:
		if !s.protected(w, r) {
			return
		}
		cards, err := s.config.SimCards.List(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"cards": cards})
	case r.Method == nethttp.MethodPost:
		var value simCardRequest
		if !s.commandJSON(w, r, &value) {
			return
		}
		if err := s.config.SimCards.Create(r.Context(), value.ICCID, value.IMSI, value.MSISDN, value.Name, value.Notes); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusCreated, map[string]string{"state": "created"})
	default:
		s.requireMethod(w, r, nethttp.MethodGet)
	}
}

func (s *Server) simCardByICCID(w nethttp.ResponseWriter, r *nethttp.Request) {
	iccid := strings.TrimPrefix(r.URL.Path, "/api/v1/simcards/")
	switch {
	case r.Method == nethttp.MethodPut:
		if !s.requireMethod(w, r, nethttp.MethodPut) || !s.protected(w, r) {
			return
		}
		var value simCardRequest
		if err := decodeJSON(r, &value); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if err := s.config.SimCards.UpdateMeta(r.Context(), iccid, value.Name, value.Notes, value.MSISDN); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]string{"state": "updated"})
	case r.Method == nethttp.MethodDelete:
		if !s.protected(w, r) {
			return
		}
		if err := s.config.SimCards.Delete(r.Context(), iccid); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]string{"state": "deleted"})
	default:
		s.requireMethod(w, r, nethttp.MethodPut)
	}
}

func (s *Server) esimConfirmationCodeReply(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	const suffix = "/confirmation-code"
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/esim/operations/")
	operationID := strings.TrimSuffix(rest, suffix)
	if rest == operationID || strings.TrimSpace(operationID) == "" {
		writeError(w, derrors.New(derrors.NotFound, "confirmation request not found", false, nil))
		return
	}
	var value struct {
		Code     string `json:"code"`
		Declined bool   `json:"declined"`
	}
	if !s.commandJSON(w, r, &value) {
		return
	}
	if err := s.config.ESIM.SubmitConfirmationCode(operationID, value.Code, value.Declined); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"state": "accepted"})
}

func (s *Server) networkStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

type networkRequest struct {
	Mode string `json:"mode"`
}

func (s *Server) networkMode(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value networkRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.Mode) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "mode is required", false, nil))
		return
	}
	id, err := s.config.Network.SetMode(r.Context(), value.Mode)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) networkCheck(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.Check(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) networkTraffic(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.Traffic(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) networkTrafficDaily(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	date := time.Now()
	if value := strings.TrimSpace(r.URL.Query().Get("date")); value != "" {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
		if err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "date must use YYYY-MM-DD", false, nil))
			return
		}
		date = parsed
	}
	value, err := s.config.Network.TrafficDaily(r.Context(), date)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) networkTrafficRange(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("range"))
	if period == "" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		writeError(w, derrors.New(derrors.InvalidRequest, "range must be day, week, or month", false, nil))
		return
	}
	value, err := s.config.Network.TrafficDailyRange(r.Context(), period, time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) networkDiagnostics(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.Diagnostics(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) calls(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("call monitoring is unavailable"))
		return
	}
	writeJSON(w, nethttp.StatusOK, s.config.Extras.Calls(r.Context()))
}

type dialRequest struct {
	Number string `json:"number"`
}

func (s *Server) callDial(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("call monitoring is unavailable"))
		return
	}
	var request dialRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, derrors.New(derrors.InvalidRequest, "number is required", false, nil))
		return
	}
	if err := s.config.Extras.Dial(r.Context(), request.Number); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]bool{"dialed": true})
}
func (s *Server) callReject(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("call monitoring is unavailable"))
		return
	}
	if err := s.config.Extras.Reject(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]bool{"rejected": true})
}
func (s *Server) esimHealth(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Device == nil || s.config.ESIM == nil {
		writeError(w, fmt.Errorf("eSIM health is unavailable"))
		return
	}
	status, err := s.config.Device.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	overview, err := s.config.ESIM.Overview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	result := map[string]any{"ok": false, "module_iccid": status.Identity.ICCID, "imsi": status.Identity.IMSI, "operator": status.Radio.Operator, "registration": status.Radio.Registered, "registered": status.Radio.Registered, "signal_dbm": status.Radio.SignalDBM, "network_mode": status.Radio.NetworkMode}
	if profiles, ok := overview["profiles"].([]backend.Profile); ok {
		for _, profile := range profiles {
			if profile.State == "enabled" {
				result["active_profile"] = profile
				result["ok"] = status.Radio.Registered && status.SIM.Inserted
				break
			}
		}
	}
	if _, ok := result["active_profile"]; !ok {
		result["message"] = "eSIM card is recognized but no enabled profile was found"
	}
	writeJSON(w, nethttp.StatusOK, result)
}

func (s *Server) esimNotes(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) || s.config.Extras == nil {
		if s.config.Extras == nil {
			writeError(w, fmt.Errorf("profile notes are unavailable"))
		}
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		value, err := s.config.Extras.Notes(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"notes": value})
	case nethttp.MethodPut:
		var request struct {
			ICCID string `json:"iccid"`
			Label string `json:"label"`
			Phone string `json:"phone"`
			Tags  string `json:"tags"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if err := s.config.Extras.SaveNote(r.Context(), request.ICCID, extras.ProfileNote{Label: request.Label, Phone: request.Phone, Tags: request.Tags}); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]string{"state": "saved"})
	default:
		s.requireMethod(w, r, nethttp.MethodGet)
	}
}

type rawATRequest struct {
	Command string `json:"command"`
}

func (s *Server) rawAT(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value rawATRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.Command) == "" {
		writeError(w, derrors.New(derrors.InvalidRequest, "command is required", false, nil))
		return
	}
	result, err := s.config.RawAT.Execute(r.Context(), value.Command)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"response": result})
}

func (s *Server) firmwareStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	value, err := s.config.Firmware.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) firmwareADBUnlock(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	id, err := s.config.Firmware.StartUnlock(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

type firmwareADBModeRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) firmwareADBMode(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value firmwareADBModeRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	id, err := s.config.Firmware.StartADBMode(r.Context(), value.Enabled)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) firmwareUSBID(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value firmware.USBIDRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	id, err := s.config.Firmware.StartUSBID(r.Context(), value)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

type firmwareModeRequest struct {
	Mode   string `json:"mode"`
	Serial string `json:"serial"`
}

func (s *Server) firmwareMode(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value firmwareModeRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if strings.TrimSpace(value.Mode) != "edl" {
		writeError(w, derrors.New(derrors.InvalidRequest, "only edl mode is supported", false, map[string]any{"mode": value.Mode}))
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	id, err := s.config.Firmware.StartEnterEDL(r.Context(), value.Serial)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

// adbShellUpgrader builds an upgrader that enforces the same loopback
// Origin/Host boundary as the event WebSocket instead of accepting any origin.
func (s *Server) adbShellUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: s.loopbackOriginAllowed}
}

func (s *Server) firmwareADBShellWS(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	if !isWebSocketRequest(r) {
		writeError(w, derrors.New(derrors.InvalidRequest, "websocket upgrade required", false, nil))
		return
	}
	serial := strings.TrimSpace(r.URL.Query().Get("serial"))
	shell, err := s.config.Firmware.OpenADBShell(serial)
	if err != nil {
		writeError(w, err)
		return
	}
	defer shell.Close()
	upgrader := s.adbShellUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buffer := make([]byte, 4096)
		for {
			count, readErr := shell.Read(buffer)
			if count > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		if _, writeErr := shell.Write(payload); writeErr != nil {
			return
		}
	}
}

func (s *Server) firmwareBackup(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value firmware.BackupRequest
	if !s.commandJSON(w, r, &value) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	id, err := s.config.Firmware.StartBackup(r.Context(), value)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) firmwareBackupSelectDirectory(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	directory, err := s.config.Firmware.SelectBackupDirectory(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"directory": directory})
}

func (s *Server) firmwareBackupSelectEDLDirectory(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	directory, err := s.config.Firmware.SelectEDLDirectory(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"directory": directory})
}

func (s *Server) firmwareSelectADBFile(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	path, err := s.config.Firmware.SelectADBFile(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"path": path})
}

func (s *Server) firmwareSetADBSettings(w nethttp.ResponseWriter, r *nethttp.Request) {
	var value struct {
		Command string `json:"command"`
	}
	if !s.commandJSON(w, r, &value) {
		return
	}
	if s.config.Firmware == nil {
		writeError(w, derrors.New(derrors.CapabilityNotSupported, "firmware management is unavailable", false, nil))
		return
	}
	if err := s.config.Firmware.SetADBCommand(r.Context(), value.Command); err != nil {
		writeError(w, err)
		return
	}
	command, source := s.config.Firmware.ADBCommandConfig()
	writeJSON(w, nethttp.StatusOK, map[string]string{"command": command, "command_source": source})
}

func (s *Server) vowifiStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.VoWiFi.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) vowifiEnable(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	id, err := s.config.VoWiFi.Enable(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}
func (s *Server) vowifiDisable(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	id, err := s.config.VoWiFi.Disable(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}
func (s *Server) vowifiReconnect(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	id, err := s.config.VoWiFi.Reconnect(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusAccepted, map[string]string{"operation_id": id})
}

func (s *Server) notificationDebug(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	if s.config.Notification == nil {
		writeError(w, fmt.Errorf("notification debug is unavailable"))
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		writeJSON(w, nethttp.StatusOK, map[string]any{
			"native_ui":   s.notificationUIAvailable(),
			"actions":     notification.DebugActions(),
			"event_drops": s.eventDropDiagnostics(),
		})
	case nethttp.MethodPost:
		var request notification.DebugRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		events, err := s.config.Notification.Debug(request)
		if err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, err.Error(), false, nil))
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{
			"action":    request.Action,
			"native_ui": s.notificationUIAvailable(),
			"events":    events,
		})
	default:
		w.Header().Set("Allow", nethttp.MethodGet+", "+nethttp.MethodPost)
		writeJSON(w, nethttp.StatusMethodNotAllowed, map[string]any{
			"error": derrors.New(derrors.InvalidRequest, "method not allowed", false, map[string]any{"method": "GET, POST"}),
		})
	}
}

func (s *Server) notificationUIAvailable() bool {
	return s.config.NotificationUIAvailable != nil && s.config.NotificationUIAvailable()
}

// eventDropDiagnostics snapshots the event-bus drop counters plus the current
// backend's event drop counter, so silent loss for a slow subscriber is
// diagnosable through the existing notification-debug response.
func (s *Server) eventDropDiagnostics() map[string]any {
	out := map[string]any{}
	if s.config.Runtime != nil && s.config.Runtime.Events() != nil {
		drops := s.config.Runtime.Events().DropCounts()
		out["cumulative"] = drops.Cumulative
		out["active_subscribers"] = drops.Active
	}
	if s.config.Runtime != nil {
		if current, err := s.config.Runtime.Backend(); err == nil {
			if counter, ok := current.(backend.EventDropCounter); ok {
				out["backends"] = map[string]any{counterMode(current): counter.EventDrops()}
			}
		}
	}
	return out
}

func counterMode(value backend.ModemBackend) string {
	if value == nil {
		return "unknown"
	}
	return value.Mode()
}

func (s *Server) notificationPermissionStatus() notification.NotificationPermissionStatus {
	if s.config.NotificationPermissionStatus != nil {
		return s.config.NotificationPermissionStatus()
	}
	return notification.NotificationPermissionStatus{State: notification.NotificationPermissionUnsupported}
}

func (s *Server) notificationPermissionPayload() map[string]any {
	status := s.notificationPermissionStatus()
	return map[string]any{
		"native_ui":         s.notificationUIAvailable(),
		"state":             status.State,
		"can_request":       status.State == notification.NotificationPermissionNotDetermined,
		"can_open_settings": status.State == notification.NotificationPermissionDenied,
	}
}

func (s *Server) notificationPermissions(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	writeJSON(w, nethttp.StatusOK, s.notificationPermissionPayload())
}

func (s *Server) requestNotificationPermission(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	accepted := s.config.RequestNotificationPermission != nil && s.config.RequestNotificationPermission()
	payload := s.notificationPermissionPayload()
	payload["accepted"] = accepted
	writeJSON(w, nethttp.StatusAccepted, payload)
}

func (s *Server) openNotificationSettings(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	accepted := s.config.OpenNotificationSettings != nil && s.config.OpenNotificationSettings()
	payload := s.notificationPermissionPayload()
	payload["accepted"] = accepted
	writeJSON(w, nethttp.StatusAccepted, payload)
}

func (s *Server) currentNotificationPreferences() notification.NotificationPreferences {
	if s.config.NotificationPreferences != nil {
		return s.config.NotificationPreferences().Normalize()
	}
	return notification.DefaultNotificationPreferences()
}

func (s *Server) notificationPreferences(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		writeJSON(w, nethttp.StatusOK, map[string]any{
			"native_ui":   s.notificationUIAvailable(),
			"preferences": s.currentNotificationPreferences(),
		})
	case nethttp.MethodPut:
		var preferences notification.NotificationPreferences
		if err := decodeJSON(r, &preferences); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if err := preferences.Validate(); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, err.Error(), false, nil))
			return
		}
		if s.config.SetNotificationPreferences == nil {
			writeError(w, fmt.Errorf("notification preferences are unavailable"))
			return
		}
		if err := s.config.SetNotificationPreferences(preferences); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{
			"native_ui":   s.notificationUIAvailable(),
			"preferences": preferences.Normalize(),
		})
	default:
		w.Header().Set("Allow", nethttp.MethodGet+", "+nethttp.MethodPut)
		writeJSON(w, nethttp.StatusMethodNotAllowed, map[string]any{
			"error": derrors.New(derrors.InvalidRequest, "method not allowed", false, map[string]any{"method": "GET, PUT"}),
		})
	}
}

func (s *Server) currentStartupStatus() startup.Status {
	if s.config.StartupStatus != nil {
		return s.config.StartupStatus()
	}
	return startup.Status{}
}

func (s *Server) startupSettings(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		writeJSON(w, nethttp.StatusOK, s.currentStartupStatus())
	case nethttp.MethodPut:
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if s.config.SetStartupEnabled == nil {
			writeError(w, fmt.Errorf("login startup is unavailable"))
			return
		}
		if err := s.config.SetStartupEnabled(request.Enabled); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, s.currentStartupStatus())
	default:
		w.Header().Set("Allow", nethttp.MethodGet+", "+nethttp.MethodPut)
		writeJSON(w, nethttp.StatusMethodNotAllowed, map[string]any{
			"error": derrors.New(derrors.InvalidRequest, "method not allowed", false, map[string]any{"method": "GET, PUT"}),
		})
	}
}

func (s *Server) commandOnly(w nethttp.ResponseWriter, r *nethttp.Request) bool {
	return s.requireMethod(w, r, nethttp.MethodPost) && s.protected(w, r)
}

func (s *Server) requireMethod(w nethttp.ResponseWriter, r *nethttp.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeJSON(w, nethttp.StatusMethodNotAllowed, map[string]any{
		"error": derrors.New(derrors.InvalidRequest, "method not allowed", false, map[string]any{"method": method}),
	})
	return false
}

func (s *Server) commandJSON(w nethttp.ResponseWriter, r *nethttp.Request, value any) bool {
	if !s.commandOnly(w, r) {
		return false
	}
	if err := decodeJSON(r, value); err != nil {
		writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
		return false
	}
	return true
}

func (s *Server) operationStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/operations/")
	if strings.TrimSpace(id) == "" {
		writeError(w, derrors.New(derrors.NotFound, "operation not found", false, nil))
		return
	}
	status, ok := s.config.Operations.Get(id)
	if !ok {
		writeError(w, derrors.New(derrors.NotFound, "operation not found", false, nil))
		return
	}
	writeJSON(w, nethttp.StatusOK, sanitizeOperationStatus(status))
}

func (s *Server) openapi(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) {
		return
	}
	writeJSON(w, nethttp.StatusOK, openAPIDocument())
}

// WebSocket keepalive and deadline policy: writeWait bounds each write,
// pongWait bounds how long a silent client may hold a session, and pingPeriod
// (shorter than pongWait) keeps healthy clients within the read deadline.
// Each Server captures them at construction so tests can shrink the windows
// per server without racing handler goroutines.
const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
)

// websocketKeepalive holds one server's captured keepalive windows.
type websocketKeepalive struct {
	write, pong, ping time.Duration
}

// eventsUpgrader enforces the same loopback Origin/Host boundary as the
// state-changing guard, without any login or credential check. It replaces
// the hand-written hijack upgrade; gorilla enforces GET and Sec-WebSocket-
// Version natively and fragments oversized frames instead of dropping them.
func (s *Server) eventsUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: s.loopbackOriginAllowed}
}

func (s *Server) websocket(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	if !isWebSocketRequest(r) {
		writeError(w, derrors.New(derrors.InvalidRequest, "websocket upgrade required", false, nil))
		return
	}
	if !s.loopbackOriginAllowed(r) {
		writeError(w, derrors.New(derrors.InvalidRequest, "websocket origin rejected", false, nil))
		return
	}
	upgrader := s.eventsUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	keepalive := s.keepalive
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(keepalive.pong))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(keepalive.pong)) })

	// Read loop: payloads are discarded; a client that misses the keepalive
	// fails the read deadline, closes the session, and releases this
	// goroutine and the event subscription.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				return
			}
		}
	}()

	// Subscribe with a watermark before building the snapshot: events
	// published during snapshot construction are queued and delivered after
	// it with ID > watermark, so the client never sees a gap under
	// client-side deduplication. The snapshot covers device status only;
	// operation, SMS, and call events are never discarded as covered.
	sub := s.config.Runtime.Events().SubscribeWithWatermark(64)
	defer sub.Unsubscribe()

	// All event and ping writes go through one writer (gorilla permits only
	// one concurrent writer).
	var writeMu sync.Mutex
	write := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(keepalive.write))
		return conn.WriteJSON(value)
	}

	// 初始快照查询失败(模组暂不可用)时不得中断事件流:连接已升级,
	// 后续 device.status.changed 事件仍会推送,前端也会定期通过 HTTP 刷新状态。
	if status, err := s.config.Device.Status(r.Context()); err == nil {
		snapshot := runtime.Event{ID: sub.Watermark, Type: "snapshot", Version: 1, OccurredAt: time.Now().UTC(), Data: sanitizeDeviceStatus(status)}
		if err := write(snapshot); err != nil {
			return
		}
	}

	pingTicker := time.NewTicker(keepalive.ping)
	defer pingTicker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-readDone:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			// An overflowing subscription has an unknown event gap: close the
			// session so the client reconnects and obtains a fresh snapshot.
			if sub.DropCount() > 0 {
				return
			}
			if err := write(sanitizeEvent(event)); err != nil {
				return
			}
		case <-pingTicker.C:
			if sub.DropCount() > 0 {
				return
			}
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(keepalive.write))
			err := conn.WriteMessage(websocket.PingMessage, nil)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func isWebSocketRequest(r *nethttp.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && r.Header.Get("Sec-WebSocket-Key") != ""
}

func decodeJSON(r *nethttp.Request, value any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(w nethttp.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w nethttp.ResponseWriter, err error) {
	structured := toStructuredError(err)
	log.Printf("http error code=%s error=%v", structured.Code, err)
	writeJSON(w, errorStatus(structured.Code), map[string]any{"error": structured})
}

type statusResponseWriter struct {
	nethttp.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = nethttp.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func logRequests(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		// WebSocket upgraders require the original ResponseWriter interfaces.
		if isWebSocketRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		recorder := &statusResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = nethttp.StatusOK
		}
		log.Printf("http request method=%s path=%s status=%d duration=%s", r.Method, r.URL.RequestURI(), status, time.Since(started).Round(time.Millisecond))
	})
}

// toStructuredError 将错误映射为结构化 API 错误。错误消息属于 API 契约
// (设备侧原因的客户端可读文本), 不受事件流净化器 (sanitize.go) 约束。
func toStructuredError(err error) *derrors.Error {
	var structured *derrors.Error
	if errors.As(err, &structured) {
		copy := *structured
		if containsCJK(copy.Message) {
			copy.Message = derrors.PublicMessage(copy.Code)
		}
		return &copy
	}
	return derrors.New(derrors.Internal, derrors.PublicMessage(derrors.Internal), true, nil)
}

func containsCJK(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func publicESIMOverview(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	if probeError, ok := copy["probe_error"].(string); ok {
		copy["probe_error"] = fallbackText(probeError, "eUICC profile probe failed")
	}
	if message, ok := copy["message"].(string); ok {
		copy["message"] = fallbackText(message, "eUICC profile service is unavailable")
	}
	return copy
}

// publicNotificationHistory 把存储层的通知历史映射为稳定 JSON 结构，
// 避免 API 面暴露 storage 类型。
func publicNotificationHistory(records []storage.NotificationHistoryRecord) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			"sequence_number": record.SequenceNumber,
			"event":           record.Event,
			"iccid":           record.ICCID,
			"address":         record.Address,
			"aid":             record.AID,
			"state":           string(record.State),
			"observed_at":     record.ObservedAt.UTC().Format(time.RFC3339Nano),
			"updated_at":      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

func errorStatus(code derrors.Code) int {
	switch code {
	case derrors.InvalidRequest:
		return nethttp.StatusBadRequest
	case derrors.Unauthenticated:
		return nethttp.StatusUnauthorized
	case derrors.NotFound:
		return nethttp.StatusNotFound
	case derrors.OperationConflict:
		return nethttp.StatusConflict
	case derrors.CapabilityNotSupported, derrors.PacketTunnelNotSupported:
		return nethttp.StatusUnprocessableEntity
	case derrors.DeviceOffline, derrors.BackendUnavailable, derrors.TransportUnavailable, derrors.Unavailable:
		return nethttp.StatusServiceUnavailable
	case derrors.OperationTimeout:
		return nethttp.StatusGatewayTimeout
	default:
		return nethttp.StatusInternalServerError
	}
}
