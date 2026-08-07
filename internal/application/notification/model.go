// Package notification owns the frozen contract between Go business logic and
// the macOS native UI bridge, plus the notification policy that decides which
// events deserve a user-facing prompt.
//
// The contract is documented in docs/native-bridge-contract.md; every event
// has a JSON fixture under testdata/ that contract_test.go decodes. Change the
// contract types only when the documented version is bumped.
package notification

import (
	"fmt"
	"time"
)

// EventVersion is the envelope version assigned by runtime.EventBus.Publish.
// All first-phase events ship at version 1.
const EventVersion = 1

// Event types shared by the WebSocket channel and the macOS native bridge.
// device.status.changed keeps its existing domain.Snapshot payload.
const (
	EventDeviceStatusChanged = "device.status.changed"
	EventDeviceOffline       = "device.offline"
	EventCallIncoming        = "call.incoming"
	EventCallUpdated         = "call.updated"
	EventCallEnded           = "call.ended"
	EventCallMissed          = "call.missed"
	EventSMSReceived         = "sms.received"
	EventSMSUpdated          = "sms.updated"
	EventNetworkUpdated      = "network.updated"
)

// Native commands are the only user actions Swift may send back to Go.
// Go executes them asynchronously and replies with a result event.
const (
	CommandRejectCall    = "reject_call"
	CommandOpenDashboard = "open_dashboard"
	// CommandNotificationPermissionStatus is an internal status update sent
	// from Swift to Go. It is not a user action and is consumed by the bridge.
	CommandNotificationPermissionStatus = "notification_permission_status"
	// CommandLog carries UI-layer trace lines back to Go so they land in the
	// same structured log pipeline as Go-side output instead of NSLog. It is
	// not a user action and is consumed by the bridge.
	CommandLog = "log"
)

// Native log levels accepted by the internal log command.
const (
	NativeLogLevelDebug = "debug"
	NativeLogLevelInfo  = "info"
	NativeLogLevelWarn  = "warn"
	NativeLogLevelError = "error"
)

// ValidNativeLogLevel reports whether level is one of the native log levels.
func ValidNativeLogLevel(level string) bool {
	switch level {
	case NativeLogLevelDebug, NativeLogLevelInfo, NativeLogLevelWarn, NativeLogLevelError:
		return true
	}
	return false
}

// Notification permission states mirror UNAuthorizationStatus without
// exposing UserNotifications types outside the macOS layer.
const (
	NotificationPermissionUnknown       = "unknown"
	NotificationPermissionNotDetermined = "not_determined"
	NotificationPermissionAuthorized    = "authorized"
	NotificationPermissionDenied        = "denied"
	NotificationPermissionProvisional   = "provisional"
	NotificationPermissionUnsupported   = "unsupported"
)

// NotificationPermissionStatus is the cached native notification permission
// state exposed to the local settings page.
type NotificationPermissionStatus struct {
	State string `json:"state"`
}

const (
	NotificationPresentationSystem = "system"
	NotificationPresentationCustom = "custom"
)

// NotificationPreferences controls the presentation surface for each prompt
// category. System uses UserNotifications; custom uses the optional AppKit
// panel in the native UI.
type NotificationPreferences struct {
	IncomingCall  string `json:"incoming_call"`
	MissedCall    string `json:"missed_call"`
	SMS           string `json:"sms"`
	DeviceOffline string `json:"device_offline"`
	ShowDebug     bool   `json:"show_debug"`
	// SenderOnly 是 macOS 通知 "仅显示发送方" 偏好: 缺失 (nil) 时通知不携带
	// 短信正文 (默认开启, 短信正文常含一次性验证码), 显式 false 表示用户
	// 选择显示正文。指针保证存在性感知, 缺失与显式 false 语义不同。
	SenderOnly *bool `json:"sender_only,omitempty"`
}

func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{
		IncomingCall:  NotificationPresentationSystem,
		MissedCall:    NotificationPresentationSystem,
		SMS:           NotificationPresentationSystem,
		DeviceOffline: NotificationPresentationSystem,
		ShowDebug:     true,
	}
}

func (p NotificationPreferences) Normalize() NotificationPreferences {
	defaults := DefaultNotificationPreferences()
	if p.IncomingCall != NotificationPresentationCustom {
		p.IncomingCall = defaults.IncomingCall
	}
	if p.MissedCall != NotificationPresentationCustom {
		p.MissedCall = defaults.MissedCall
	}
	if p.SMS != NotificationPresentationCustom {
		p.SMS = defaults.SMS
	}
	if p.DeviceOffline != NotificationPresentationCustom {
		p.DeviceOffline = defaults.DeviceOffline
	}
	// SenderOnly 原样保留: nil 表示缺失 (macOS 端默认开启), 显式 false 保留。
	return p
}

func (p NotificationPreferences) Validate() error {
	for name, value := range map[string]string{
		"incoming_call":  p.IncomingCall,
		"missed_call":    p.MissedCall,
		"sms":            p.SMS,
		"device_offline": p.DeviceOffline,
	} {
		if value != NotificationPresentationSystem && value != NotificationPresentationCustom {
			return fmt.Errorf("notification preference %s has unsupported mode %q", name, value)
		}
	}
	return nil
}

func ValidNotificationPermissionState(state string) bool {
	switch state {
	case NotificationPermissionUnknown,
		NotificationPermissionNotDetermined,
		NotificationPermissionAuthorized,
		NotificationPermissionDenied,
		NotificationPermissionProvisional,
		NotificationPermissionUnsupported:
		return true
	default:
		return false
	}
}

// Command result events published by Go after executing a native command.
const (
	EventCallRejectStarted                  = "call.reject.started"
	EventCallRejectSucceeded                = "call.reject.succeeded"
	EventCallRejectFailed                   = "call.reject.failed"
	EventDashboardOpened                    = "dashboard.opened"
	EventNotificationPermissionRequest      = "notification.permission.request"
	EventNotificationPermissionOpenSettings = "notification.permission.open_settings"
	EventNotificationPreferencesUpdated     = "notification.preferences.updated"
	// EventCommandDropped is published when a Swift-to-Go command could not be
	// enqueued; it names the command and the reason so the UI can recover the
	// pending user action instead of remaining stuck.
	EventCommandDropped = "command.dropped"
)

// Debug actions are deliberately finite. They exercise the same EventBus
// path as production producers without allowing the HTTP API to inject an
// arbitrary event or payload into the native bridge.
const (
	DebugCallIncoming     = "call_incoming"
	DebugCallUpdated      = "call_updated"
	DebugCallEnded        = "call_ended"
	DebugCallMissed       = "call_missed"
	DebugSMSReceived      = "sms_received"
	DebugDeviceOffline    = "device_offline"
	DebugDeviceReady      = "device_ready"
	DebugNetworkConnected = "network_connected"
	DebugNetworkWeak      = "network_weak"
	DebugNetworkOffline   = "network_offline"
)

// DebugRequest describes one notifier scenario requested by the local debug
// page. CallID is reused by the call lifecycle actions.
type DebugRequest struct {
	Action    string `json:"action"`
	CallID    string `json:"call_id,omitempty"`
	Number    string `json:"number,omitempty"`
	Sender    string `json:"sender,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Body      string `json:"body,omitempty"`
}

// DebugAction describes one supported debug scenario for the management UI.
type DebugAction struct {
	Action string `json:"action"`
	Event  string `json:"event"`
	Count  int    `json:"count,omitempty"`
}

// DebugActions returns a new slice so API callers cannot mutate the catalog.
func DebugActions() []DebugAction {
	return []DebugAction{
		{Action: DebugCallIncoming, Event: EventCallIncoming},
		{Action: DebugCallUpdated, Event: EventCallUpdated},
		{Action: DebugCallEnded, Event: EventCallEnded},
		{Action: DebugCallMissed, Event: EventCallMissed},
		{Action: DebugSMSReceived, Event: EventSMSReceived},
		{Action: DebugDeviceOffline, Event: EventDeviceOffline, Count: OfflineErrorThreshold},
		{Action: DebugDeviceReady, Event: EventDeviceStatusChanged},
		{Action: DebugNetworkConnected, Event: EventNetworkUpdated},
		{Action: DebugNetworkWeak, Event: EventNetworkUpdated},
		{Action: DebugNetworkOffline, Event: EventNetworkUpdated},
	}
}

// CallEvent is the payload of call.incoming / call.updated / call.ended /
// call.missed. It is a copy of extras.CallRecord restricted to fields the
// native UI displays; no backend or AT types cross the bridge.
type CallEvent struct {
	ID          string     `json:"id"`
	Direction   string     `json:"direction"` // incoming | outgoing
	State       string     `json:"state"`     // incoming | waiting | active | alerting | dialing | held
	Number      string     `json:"number,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	Missed      bool       `json:"missed"`
	// NotificationEligible is internal reconciliation policy state. EventBus
	// call events are already authoritative; only state-based recovery checks
	// this field. It must never cross the native bridge contract.
	NotificationEligible bool `json:"-"`
}

// SMSMessageEvent is the payload of sms.received. DedupKey() follows the
// documented dedup key: sender + recipient + body + received_at.
type SMSMessageEvent struct {
	Index      int       `json:"index"`
	Sender     string    `json:"sender,omitempty"`
	Recipient  string    `json:"recipient,omitempty"`
	Body       string    `json:"body"`
	ReceivedAt time.Time `json:"received_at"`
	// RecordedAt is the device-local moment the message was first recorded;
	// the ordering key shared with the SMS list API.
	RecordedAt time.Time `json:"recorded_at,omitempty"`
	// ICCID is the SIM identity the message was recorded under.
	ICCID string `json:"iccid,omitempty"`
}

// DedupKey returns the stable key used to show each short message once.
func (m SMSMessageEvent) DedupKey() string {
	return m.Sender + "\x00" + m.Recipient + "\x00" + m.Body + "\x00" + m.ReceivedAt.UTC().Format(time.RFC3339Nano)
}

// DeviceOfflineEvent is the payload of device.offline, published when the
// device runtime enters a disconnected/offline state.
type DeviceOfflineEvent struct {
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// NetworkUpdateEvent is the payload of network.updated published by new
// producers; it drives the 4G menu bar model. Existing producers keep their
// legacy map payloads until they are converged (see the contract doc).
type NetworkUpdateEvent struct {
	Mode        string `json:"mode,omitempty"`
	NetworkMode string `json:"network_mode,omitempty"`
	Registered  bool   `json:"registered"`
	SIMInserted bool   `json:"sim_inserted"`
	SIMKnown    bool   `json:"sim_known,omitempty"`
	Operator    string `json:"operator,omitempty"`
	SignalDBM   int    `json:"signal_dbm,omitempty"`
}

// Command is a user action crossing from Swift to Go. Params carries command
// specific values, e.g. reject_call -> {"call_id": "..."}.
type Command struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}

func (c Command) Param(key string) string {
	if c.Params == nil {
		return ""
	}
	return c.Params[key]
}

// CallID returns the target of a reject_call command, or "" when absent.
func (c Command) CallID() string { return c.Param("call_id") }

// IsRejectCall reports whether c is a well-formed reject_call command.
func (c Command) IsRejectCall() bool { return c.Name == CommandRejectCall && c.CallID() != "" }

// RejectResult is the payload of call.reject.started / .succeeded / .failed.
type RejectResult struct {
	CallID string `json:"call_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// DashboardOpened is the payload of dashboard.opened.
type DashboardOpened struct {
	URL string `json:"url"`
}

// CommandDropped is the payload of command.dropped: a Swift-to-Go command that
// could not be enqueued. Reason is a short machine-readable cause such as
// "queue_full". Go never emits a started/succeeded result for a dropped
// command, so this feedback is the only way the UI learns the action failed to
// leave the device.
type CommandDropped struct {
	Command string `json:"command"`
	Reason  string `json:"reason"`
}
