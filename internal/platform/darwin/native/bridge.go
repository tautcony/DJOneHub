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

	mu       sync.Mutex
	commands chan notification.Command
	ready    chan struct{}
	started  bool
	exited   bool
	eventSeq atomic.Uint64

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
	b.sendEvent(notification.EventCallIncoming, call)
}

func (b *Bridge) UpdateCall(call notification.CallEvent) {
	b.sendEvent(notification.EventCallUpdated, call)
}

func (b *Bridge) ShowMissedCall(call notification.CallEvent) {
	b.sendEvent(notification.EventCallMissed, call)
}

func (b *Bridge) ShowSMS(message notification.SMSMessageEvent) {
	b.sendEvent(notification.EventSMSReceived, message)
}

func (b *Bridge) ShowOffline(event notification.DeviceOfflineEvent) {
	b.sendEvent(notification.EventDeviceOffline, event)
}

func (b *Bridge) HideCall(call notification.CallEvent) {
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
	b.mu.Lock()
	commands := b.commands
	b.mu.Unlock()
	if commands == nil {
		return
	}
	select {
	case commands <- command:
	default:
		// A slow command consumer must not block the UI thread.
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
