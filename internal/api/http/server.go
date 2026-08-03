package httpapi

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/extras"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

type Authenticator interface{ Authenticate(*nethttp.Request) bool }
type AuthenticatorFunc func(*nethttp.Request) bool

func (f AuthenticatorFunc) Authenticate(r *nethttp.Request) bool { return f(r) }

type Config struct {
	Device                        *device.Service
	SMS                           *sms.Service
	ESIM                          *esim.Service
	Network                       *network.Service
	Notification                  *notification.Service
	RawAT                         *rawat.Service
	VoWiFi                        *vowifi.Service
	Extras                        *extras.Service
	Operations                    *operation.Manager
	Runtime                       *runtime.Runtime
	Auth                          Authenticator
	NotificationUIAvailable       func() bool
	NotificationPermissionStatus  func() notification.NotificationPermissionStatus
	RequestNotificationPermission func() bool
	OpenNotificationSettings      func() bool
	NotificationPreferences       func() notification.NotificationPreferences
	SetNotificationPreferences    func(notification.NotificationPreferences) error
}

type Server struct{ config Config }

func NewServer(config Config) *Server { return &Server{config: config} }

func (s *Server) Handler() nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/api/v1/device", s.deviceStatus)
	mux.HandleFunc("/api/v1/device/capabilities", s.deviceCapabilities)
	mux.HandleFunc("/api/v1/device/status", s.deviceStatus)
	mux.HandleFunc("/api/v1/device/actions/rescan", s.rescan)
	mux.HandleFunc("/api/v1/device/actions/reboot", s.reboot)
	mux.HandleFunc("/api/v1/sms", s.smsList)
	mux.HandleFunc("/api/v1/sms/actions/refresh", s.smsRefresh)
	mux.HandleFunc("/api/v1/sms/actions/send", s.smsSend)
	mux.HandleFunc("/api/v1/sms/actions/clear", s.smsClear)
	mux.HandleFunc("/api/v1/esim", s.esimOverview)
	mux.HandleFunc("/api/v1/esim/actions/download", s.esimDownload)
	mux.HandleFunc("/api/v1/esim/actions/enable", s.esimEnable)
	mux.HandleFunc("/api/v1/esim/actions/rename", s.esimRename)
	mux.HandleFunc("/api/v1/esim/actions/delete", s.esimDelete)
	mux.HandleFunc("/api/v1/network", s.networkStatus)
	mux.HandleFunc("/api/v1/network/actions/mode", s.networkMode)
	mux.HandleFunc("/api/v1/network/actions/check", s.networkCheck)
	mux.HandleFunc("/api/v1/network/actions/traffic", s.networkTraffic)
	mux.HandleFunc("/api/v1/network/diagnostics", s.networkDiagnostics)
	mux.HandleFunc("/api/v1/network/actions/check-4g", s.networkCheck4G)
	mux.HandleFunc("/api/v1/network/actions/check-proxy", s.networkCheckProxy)
	mux.HandleFunc("/api/v1/network/policy", s.networkPolicy)
	mux.HandleFunc("/api/v1/network/actions/policy", s.networkPolicySet)
	mux.HandleFunc("/api/v1/device/actions/raw-at", s.rawAT)
	mux.HandleFunc("/api/v1/vowifi", s.vowifiStatus)
	mux.HandleFunc("/api/v1/vowifi/actions/enable", s.vowifiEnable)
	mux.HandleFunc("/api/v1/vowifi/actions/disable", s.vowifiDisable)
	mux.HandleFunc("/api/v1/vowifi/actions/reconnect", s.vowifiReconnect)
	mux.HandleFunc("/api/v1/notifications/debug", s.notificationDebug)
	mux.HandleFunc("/api/v1/notifications/permissions", s.notificationPermissions)
	mux.HandleFunc("/api/v1/notifications/permissions/request", s.requestNotificationPermission)
	mux.HandleFunc("/api/v1/notifications/permissions/open-settings", s.openNotificationSettings)
	mux.HandleFunc("/api/v1/notifications/preferences", s.notificationPreferences)
	mux.HandleFunc("/api/v1/calls", s.calls)
	mux.HandleFunc("/api/v1/calls/actions/reject", s.callReject)
	mux.HandleFunc("/api/v1/gps", s.gps)
	mux.HandleFunc("/api/v1/gps/actions/start", s.gpsStart)
	mux.HandleFunc("/api/v1/gps/actions/stop", s.gpsStop)
	mux.HandleFunc("/api/v1/gps/actions/refresh", s.gpsRefresh)
	mux.HandleFunc("/api/v1/esim/health", s.esimHealth)
	mux.HandleFunc("/api/v1/esim/notes", s.esimNotes)
	mux.HandleFunc("/api/v1/operations/", s.operationStatus)
	mux.HandleFunc("/api/v1/openapi.json", s.openapi)
	mux.HandleFunc("/api/v1/events/ws", s.websocket)
	return mux
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
	writeJSON(w, nethttp.StatusOK, publicDeviceStatus(status))
}

func (s *Server) deviceCapabilities(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	snapshot := publicSnapshot(s.config.Runtime.Snapshot())
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
	writeJSON(w, nethttp.StatusOK, map[string]any{"state": publicSnapshot(s.config.Runtime.Snapshot())})
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

func (s *Server) smsList(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	items, err := s.config.SMS.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
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

func (s *Server) networkCheck4G(w nethttp.ResponseWriter, r *nethttp.Request) {
	s.networkRouteCheck(w, r, "4g")
}
func (s *Server) networkCheckProxy(w nethttp.ResponseWriter, r *nethttp.Request) {
	s.networkRouteCheck(w, r, "proxy")
}
func (s *Server) networkRouteCheck(w nethttp.ResponseWriter, r *nethttp.Request, kind string) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.CheckRoute(r.Context(), kind)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) networkPolicy(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	value, err := s.config.Network.CellularPolicy(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}
func (s *Server) networkPolicySet(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodPost) || !s.protected(w, r) {
		return
	}
	var request struct {
		ForceOff bool `json:"force_off"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
		return
	}
	value, err := s.config.Network.SetCellularPolicy(r.Context(), request.ForceOff)
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
func (s *Server) gps(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("GPS is unavailable"))
		return
	}
	writeJSON(w, nethttp.StatusOK, s.config.Extras.GPS(r.Context()))
}
func (s *Server) gpsStart(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("GPS is unavailable"))
		return
	}
	value, err := s.config.Extras.StartGPS(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}
func (s *Server) gpsStop(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("GPS is unavailable"))
		return
	}
	value, err := s.config.Extras.StopGPS(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}
func (s *Server) gpsRefresh(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.commandOnly(w, r) {
		return
	}
	if s.config.Extras == nil {
		writeError(w, fmt.Errorf("GPS is unavailable"))
		return
	}
	value, err := s.config.Extras.RefreshGPS(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, value)
}

func (s *Server) esimHealth(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.Extras == nil {
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
			"native_ui": s.notificationUIAvailable(),
			"actions":   notification.DebugActions(),
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
	writeJSON(w, nethttp.StatusOK, publicOperationStatus(status))
}

func (s *Server) openapi(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) {
		return
	}
	writeJSON(w, nethttp.StatusOK, openAPIDocument())
}

func (s *Server) websocket(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.protected(w, r) {
		return
	}
	if !isWebSocketRequest(r) {
		writeError(w, derrors.New(derrors.InvalidRequest, "websocket upgrade required", false, nil))
		return
	}
	hijacker, ok := w.(nethttp.Hijacker)
	if !ok {
		writeError(w, derrors.New(derrors.Internal, "websocket is unavailable", false, nil))
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	key := r.Header.Get("Sec-WebSocket-Key")
	accept := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:]))
	_ = rw.Flush()
	status, err := s.config.Device.Status(r.Context())
	if err != nil {
		return
	}
	snapshot := runtime.Event{ID: s.config.Runtime.Events().LastID(), Type: "snapshot", Version: 1, OccurredAt: time.Now().UTC(), Data: publicDeviceStatus(status)}
	if err := writeTextFrame(rw, snapshot); err != nil {
		return
	}
	_, events, unsubscribe := s.config.Runtime.Events().Subscribe(32)
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeTextFrame(rw, publicEvent(event)); err != nil {
				return
			}
		}
	}
}

func isWebSocketRequest(r *nethttp.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && r.Header.Get("Sec-WebSocket-Key") != ""
}

func writeTextFrame(w *bufio.ReadWriter, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var header []byte
	switch {
	case len(payload) < 126:
		header = []byte{0x81, byte(len(payload))}
	case len(payload) <= 65535:
		header = []byte{0x81, 126, byte(len(payload) >> 8), byte(len(payload))}
	default:
		return fmt.Errorf("event frame is too large")
	}
	frame := append(header, payload...)
	if _, err := w.Write(frame); err != nil {
		return err
	}
	return w.Flush()
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
	writeJSON(w, errorStatus(structured.Code), map[string]any{"error": structured})
}

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

func publicDeviceStatus(value device.Status) device.Status {
	value.Snapshot = publicSnapshot(value.Snapshot)
	return value
}

func publicSnapshot(value domain.Snapshot) domain.Snapshot {
	value.BackendReason = publicText(value.BackendReason, "backend selection failed")
	value.LastError = publicText(value.LastError, "device error")
	if value.Capabilities != nil {
		capabilities := make(domain.CapabilitySet, len(value.Capabilities))
		for capability, reason := range value.Capabilities {
			capabilities[capability] = publicText(reason, "capability is unavailable")
		}
		value.Capabilities = capabilities
	}
	return value
}

func publicOperationStatus(value operation.Status) operation.Status {
	if value.Error != nil {
		value.Error = toStructuredError(value.Error)
	}
	return value
}

func publicESIMOverview(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	if probeError, ok := copy["probe_error"].(string); ok {
		copy["probe_error"] = publicText(probeError, "eUICC profile probe failed")
	}
	if message, ok := copy["message"].(string); ok {
		copy["message"] = publicText(message, "eUICC profile service is unavailable")
	}
	return copy
}

func publicEvent(value runtime.Event) runtime.Event {
	switch data := value.Data.(type) {
	case device.Status:
		value.Data = publicDeviceStatus(data)
	case domain.Snapshot:
		value.Data = publicSnapshot(data)
	case operation.Status:
		value.Data = publicOperationStatus(data)
	case *operation.Status:
		if data != nil {
			copy := publicOperationStatus(*data)
			value.Data = copy
		}
	}
	return value
}

func publicText(value, fallback string) string {
	if containsCJK(value) {
		return fallback
	}
	return value
}

func containsCJK(value string) bool {
	for _, r := range value {
		if (r >= '\u3400' && r <= '\u4dbf') || (r >= '\u4e00' && r <= '\u9fff') {
			return true
		}
	}
	return false
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
	case derrors.DeviceOffline, derrors.BackendUnavailable, derrors.TransportUnavailable:
		return nethttp.StatusServiceUnavailable
	case derrors.OperationTimeout:
		return nethttp.StatusGatewayTimeout
	default:
		return nethttp.StatusInternalServerError
	}
}
