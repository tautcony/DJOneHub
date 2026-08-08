// Package native hosts the macOS UI bridge inside the Go process. The Swift
// UI layer (SwiftUI/AppKit/MapKit) is linked into the Go binary as a static
// library; Go drives it through the C ABI in bridge.h and receives user
// actions back as JSON commands. Non-darwin builds use a no-op stub so the
// rest of the app compiles and runs without AppKit.
package native

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/pkg/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// CommandHandler executes native UI commands sent from Swift. The app layer
// wires it in; the bridge validates commands before dispatch.
type CommandHandler interface {
	HandleCommand(ctx context.Context, command notification.Command)
}

// uiDriver is the platform boundary implemented by the cgo bridge on darwin
// and a no-op stub elsewhere.
type uiDriver interface {
	// start runs the native UI on the current OS thread until it exits.
	start(configJSON string, bridge *Bridge)
	handleEvent(eventJSON string)
	stop()
	hasUI() bool
}

// Bridge drives the native UI host inside this process. It implements
// notification.Sink so it can be wired to the notification policy directly.
type Bridge struct {
	handler CommandHandler
	driver  uiDriver
	webURL  string

	mu           sync.Mutex
	commands     chan notification.Command
	ready        chan struct{}
	started      bool
	exited       bool
	eventSeq     atomic.Uint64
	commandDrops atomic.Uint64

	permissionMu     sync.RWMutex
	permissionStatus notification.NotificationPermissionStatus
	preferencesMu    sync.RWMutex
	preferences      notification.NotificationPreferences
}

// HasUI reports whether this process hosts a native UI (darwin + cgo).
func (b *Bridge) HasUI() bool { return b.driver.hasUI() }

// WebURL returns the dashboard URL configured at Start; used by the command
// handler to answer open_dashboard commands.
func (b *Bridge) WebURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.webURL
}

// SetHandler installs or replaces the command handler. Needed when the
// handler and the bridge reference each other.
func (b *Bridge) SetHandler(handler CommandHandler) {
	b.mu.Lock()
	b.handler = handler
	b.mu.Unlock()
}

// New creates a bridge for the running platform. handler may be nil; commands
// are then validated and dropped.
func New(handler CommandHandler) *Bridge {
	driver := newDriver()
	return newBridge(handler, driver)
}

// newWithDriver is the test hook for injecting a fake driver.
func newWithDriver(handler CommandHandler, driver uiDriver) *Bridge {
	return newBridge(handler, driver)
}

func newBridge(handler CommandHandler, driver uiDriver) *Bridge {
	state := notification.NotificationPermissionUnknown
	if !driver.hasUI() {
		state = notification.NotificationPermissionUnsupported
	}
	return &Bridge{
		handler:          handler,
		driver:           driver,
		permissionStatus: notification.NotificationPermissionStatus{State: state},
		preferences:      notification.DefaultNotificationPreferences(),
	}
}

// Ready returns a channel closed once the UI has finished launching. It is
// never closed when the platform has no UI (stub driver).
func (b *Bridge) Ready() <-chan struct{} {
	return b.ready
}

// Start launches the UI and blocks until the UI run loop exits. On macOS the
// caller must pin the main OS thread with runtime.LockOSThread and call Start
// on that goroutine; native_ui_start needs the process main thread.
func (b *Bridge) Start(ctx context.Context, webURL string) error {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.webURL = webURL
	commands := make(chan notification.Command, 16)
	b.commands = commands
	ready := make(chan struct{})
	b.ready = ready
	b.mu.Unlock()

	go b.commandLoop(ctx, commands)
	preferences := b.NotificationPreferences()
	config := struct {
		WebURL      string                               `json:"web_url"`
		Preferences notification.NotificationPreferences `json:"notification_preferences"`
	}{WebURL: webURL, Preferences: preferences}
	encodedConfig, _ := json.Marshal(config)
	b.driver.start(string(encodedConfig), b)

	b.mu.Lock()
	b.exited = true
	b.mu.Unlock()
	return nil
}

// Stop requests the UI run loop to exit. Safe to call multiple times and
// before Start.
func (b *Bridge) Stop() {
	b.driver.stop()
}

// NotificationPermissionStatus returns the latest state reported by the
// native notification center. The initial state is unknown until the UI has
// finished launching, or unsupported when this process has no native UI.
func (b *Bridge) NotificationPermissionStatus() notification.NotificationPermissionStatus {
	b.permissionMu.RLock()
	defer b.permissionMu.RUnlock()
	return b.permissionStatus
}

// RequestNotificationPermission asks the native UI to show Apple's permission
// prompt. It is intentionally asynchronous because the prompt is controlled
// by macOS and the result is reported through the cached status.
func (b *Bridge) RequestNotificationPermission() bool {
	if !b.canControlNativeUI() {
		return false
	}
	b.Send(notification.EventNotificationPermissionRequest, nil)
	return true
}

// OpenNotificationSettings opens the macOS Notifications settings pane for
// this app after the user has previously denied notification access.
func (b *Bridge) OpenNotificationSettings() bool {
	if !b.canControlNativeUI() {
		return false
	}
	b.Send(notification.EventNotificationPermissionOpenSettings, nil)
	return true
}

// NotificationPreferences returns the current presentation preference for
// each native notification category.
func (b *Bridge) NotificationPreferences() notification.NotificationPreferences {
	b.preferencesMu.RLock()
	defer b.preferencesMu.RUnlock()
	return b.preferences
}

// SetNotificationPreferences updates the native presentation policy. Before
// the UI starts, the value is included in the startup config; afterwards an
// event updates the live Swift coordinator.
func (b *Bridge) SetNotificationPreferences(preferences notification.NotificationPreferences) {
	preferences = preferences.Normalize()
	b.preferencesMu.Lock()
	b.preferences = preferences
	b.preferencesMu.Unlock()
	if b.canControlNativeUI() {
		b.Send(notification.EventNotificationPreferencesUpdated, preferences)
	}
}

func (b *Bridge) canControlNativeUI() bool {
	b.mu.Lock()
	started := b.started && !b.exited
	b.mu.Unlock()
	return started && b.driver.hasUI()
}

func (b *Bridge) updateNotificationPermissionStatus(state string) {
	if !notification.ValidNotificationPermissionState(state) {
		return
	}
	b.permissionMu.Lock()
	b.permissionStatus = notification.NotificationPermissionStatus{State: state}
	b.permissionMu.Unlock()
}

// Send delivers an application event to the UI outside the notification
// policy path (e.g. command result events produced by the command handler).
func (b *Bridge) Send(eventType string, data any) {
	event := runtime.Event{
		ID:         b.eventSeq.Add(1),
		Type:       eventType,
		Version:    notification.EventVersion,
		OccurredAt: time.Now().UTC(),
		Data:       data,
	}
	b.send(event)
}

// Sink implementation: policy-approved prompts forwarded to the UI.

func (b *Bridge) UpdateDeviceStatus(snapshot device.Snapshot) {
	b.sendEvent(notification.EventDeviceStatusChanged, snapshot)
}

func (b *Bridge) ShowCall(call notification.CallEvent) {
	logger.Info("[native] forward call event", "event", notification.EventCallIncoming, "call_id", call.ID, "state", call.State, "number", call.Number)
	b.sendEvent(notification.EventCallIncoming, call)
}

func (b *Bridge) UpdateCall(call notification.CallEvent) {
	logger.Info("[native] forward call event", "event", notification.EventCallUpdated, "call_id", call.ID, "state", call.State, "number", call.Number)
	b.sendEvent(notification.EventCallUpdated, call)
}

func (b *Bridge) ShowMissedCall(call notification.CallEvent) {
	logger.Info("[native] forward call event", "event", notification.EventCallMissed, "call_id", call.ID, "state", call.State, "number", call.Number)
	b.sendEvent(notification.EventCallMissed, call)
}

func (b *Bridge) ShowSMS(message notification.SMSMessageEvent) {
	b.sendEvent(notification.EventSMSReceived, message)
}

func (b *Bridge) ShowOffline(event notification.DeviceOfflineEvent) {
	b.sendEvent(notification.EventDeviceOffline, event)
}

func (b *Bridge) HideCall(call notification.CallEvent) {
	logger.Info("[native] forward call event", "event", notification.EventCallEnded, "call_id", call.ID, "state", call.State)
	b.sendEvent(notification.EventCallEnded, call)
}

func (b *Bridge) UpdateNetwork(state notification.NetworkUpdateEvent) {
	b.sendEvent(notification.EventNetworkUpdated, state)
}

func (b *Bridge) sendEvent(eventType string, data any) {
	event := runtime.Event{
		ID:         b.eventSeq.Add(1),
		Type:       eventType,
		Version:    notification.EventVersion,
		OccurredAt: time.Now().UTC(),
		Data:       data,
	}
	b.send(event)
}

func (b *Bridge) send(event runtime.Event) {
	b.mu.Lock()
	started := b.started && !b.exited
	b.mu.Unlock()
	if !started {
		// The AppKit run loop is not running (or already stopped): posting an
		// event into it is invalid, so the event is dropped here instead.
		return
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	b.driver.handleEvent(string(encoded))
}

func (b *Bridge) enqueueCommand(jsonString string) {
	var command notification.Command
	if err := json.Unmarshal([]byte(jsonString), &command); err != nil {
		return
	}
	if notification.ValidateCommand(command) != nil {
		return
	}
	if command.Name == notification.CommandNotificationPermissionStatus {
		b.updateNotificationPermissionStatus(command.Param("state"))
		return
	}
	if command.Name == notification.CommandLog {
		// UI-layer traces are written to the structured pipeline on a fresh
		// goroutine, matching the bridge threading contract; they never reach
		// the command handler.
		go logFromNative(command)
		return
	}
	b.mu.Lock()
	commands := b.commands
	b.mu.Unlock()
	if commands == nil {
		return
	}
	select {
	case commands <- command:
	default:
		// A slow command consumer must not block the UI thread, and a lost
		// user command must not disappear silently: report it back to Swift
		// and log it instead of pretending the command was accepted.
		logger.Warn("native command dropped", "command", command.Name, "reason", "queue_full")
		b.commandDrops.Add(1)
		b.Send(notification.EventCommandDropped, notification.CommandDropped{
			Command: command.Name,
			Reason:  "queue_full",
		})
	}
}

type Diagnostics struct {
	Available     bool   `json:"available"`
	Running       bool   `json:"running"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
	Dropped       uint64 `json:"dropped"`
}

func (b *Bridge) Diagnostics() Diagnostics {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := Diagnostics{Available: b.driver.hasUI(), Running: b.started && !b.exited, Dropped: b.commandDrops.Load()}
	if b.commands != nil {
		out.QueueDepth, out.QueueCapacity = len(b.commands), cap(b.commands)
	}
	return out
}

// nativeLogLevels maps the contract log levels to zap levels.
var nativeLogLevels = map[string]zapcore.Level{
	notification.NativeLogLevelDebug: zapcore.DebugLevel,
	notification.NativeLogLevelInfo:  zapcore.InfoLevel,
	notification.NativeLogLevelWarn:  zapcore.WarnLevel,
	notification.NativeLogLevelError: zapcore.ErrorLevel,
}

// logFromNative writes a UI-layer trace into the structured logger. The
// message is a constant and the dynamic values ride as structured fields, so
// formatting is deferred to this side: the level filter runs before any field
// is encoded, and a filtered level costs nothing but the transport. The level
// is contract-validated by ValidateCommand before this runs; unknown levels
// fall back to info.
func logFromNative(command notification.Command) {
	level, ok := nativeLogLevels[command.Param("level")]
	if !ok {
		level = zapcore.InfoLevel
	}
	entry := zapcore.Entry{Level: level, Time: time.Now(), Message: command.Param("message")}
	core := logger.ZapLogger().Core()
	if ce := core.Check(entry, nil); ce == nil {
		return
	}
	var fields []zapcore.Field
	for key, value := range command.Params {
		if key != "level" && key != "message" {
			fields = append(fields, zap.String(key, value))
		}
	}
	if ce := core.Check(entry, nil); ce != nil {
		ce.Write(fields...)
	}
}

func (b *Bridge) commandLoop(ctx context.Context, commands <-chan notification.Command) {
	for {
		select {
		case <-ctx.Done():
			return
		case command, ok := <-commands:
			if !ok {
				return
			}
			b.mu.Lock()
			handler := b.handler
			b.mu.Unlock()
			if handler != nil {
				handler.HandleCommand(ctx, command)
			}
		}
	}
}
