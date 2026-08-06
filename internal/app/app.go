package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	httpapi "github.com/iniwex5/vohive/internal/api/http"
	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/extras"
	"github.com/iniwex5/vohive/internal/application/firmware"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	djiesim "github.com/iniwex5/vohive/internal/esim"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/platform/darwin"
	"github.com/iniwex5/vohive/internal/platform/darwin/native"
	"github.com/iniwex5/vohive/internal/platform/linux"
	"github.com/iniwex5/vohive/internal/platform/startup"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/platform/windows"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/transport"
)

// serialESIMPortBuilder 为串口 AT 路径（Linux/Windows）构建 eSIM 服务端口。
// 模组暴露为操作系统串口，eUICC APDU 通道走 modem.Manager 的串口 AT 通道
// （AT+CCHO/AT+CGLA/AT+CCHC），与 darwin 的 USB AT 路径共用 internal/esim.NewATPort。
// arbiter 与 modem manager 共享同一设备级 APDU 仲裁器实例。
func serialESIMPortBuilder() func(*modem.Manager, *apduarbiter.Arbiter, domain.Candidate) (backend.ESIMPort, error) {
	return func(m *modem.Manager, arbiter *apduarbiter.Arbiter, candidate domain.Candidate) (backend.ESIMPort, error) {
		return djiesim.NewATPort(
			candidate.Identity.StableID,
			arbiter,
			func(cmd string, timeout time.Duration) (string, error) { return m.ExecuteAT(cmd, timeout) },
			func(context.Context) (string, error) { return m.QueryIMEI() },
			func(context.Context) (string, error) { return m.QueryICCID() },
		)
	}
}

type noHardwareDiscovery struct{}

func (noHardwareDiscovery) Discover(context.Context) ([]domain.Candidate, error) { return nil, nil }

type noHardwareBackends struct{}

func (noHardwareBackends) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return nil, "", fmt.Errorf("no modem backend is configured")
}

type App struct {
	Runtime      *runtime.Runtime
	Operations   *operation.Manager
	Device       *device.Service
	SMS          *sms.Service
	ESIM         *esim.Service
	Network      *network.Service
	RawAT        *rawat.Service
	VoWiFi       *vowifi.Service
	Extras       *extras.Service
	Firmware     *firmware.Service
	Notification *notification.Service
	NativeUI     *native.Bridge
	HTTP         *httpapi.Server
	Store        *storage.SQLiteStore

	// shutdownAdmission is closed by BeginShutdown; while open the HTTP
	// server admits requests and the operation manager accepts new work.
	shutdownAdmission chan struct{}
	admissionOnce     sync.Once
}

func NewOffline() (*App, error) {
	r, err := runtime.New(runtime.Config{Discovery: noHardwareDiscovery{}, Backends: noHardwareBackends{}})
	return newApp(r, err, unsupported.New("offline", domain.CapabilitySet{}))
}

// New selects the real platform adapter for the running process. The offline
// constructor remains available for demos and tests that must not inspect
// hardware.
func New() (*App, error) {
	var discovery transport.DeviceDiscovery
	var backends backend.BackendFactory
	var networkAdapter transport.NetworkController
	switch goruntime.GOOS {
	case "darwin":
		adapter := darwin.New()
		discovery, backends, networkAdapter = adapter, backend.NewATFactory(adapter.OpenAT), adapter
	case "linux":
		adapter := linux.New()
		factory := backend.NewATFactory(nil)
		factory.ESIMPort = serialESIMPortBuilder()
		discovery, backends, networkAdapter = adapter, factory, adapter
	case "windows":
		adapter := windows.New()
		factory := backend.NewATFactory(nil)
		factory.ESIMPort = serialESIMPortBuilder()
		discovery, backends, networkAdapter = adapter, factory, adapter
	default:
		return NewOffline()
	}
	r, err := runtime.New(runtime.Config{Discovery: discovery, Backends: backends})
	return newApp(r, err, networkAdapter)
}

func newApp(r *runtime.Runtime, err error, platformAdapter transport.NetworkController) (*App, error) {
	if err != nil {
		return nil, err
	}
	storeDir, _ := os.UserConfigDir()
	if storeDir == "" {
		storeDir = "."
	}
	database, err := storage.OpenSQLite(filepath.Join(storeDir, "DJOneHub", "djonehub.sqlite3"))
	if err != nil {
		return nil, err
	}
	profileStore := database.Namespace("profile_notes")
	if exists, _ := profileStore.Exists(); !exists {
		var notes map[string]extras.ProfileNote
		legacy := storage.NewJSONStore(filepath.Join(storeDir, "DJOneHub", "profile-notes.json"))
		if legacy.Read(&notes) == nil && notes != nil {
			_ = profileStore.Write(&notes)
		}
	}
	notificationPreferencesStore := database.Namespace("notification_preferences")
	if exists, _ := notificationPreferencesStore.Exists(); !exists {
		legacy := storage.NewJSONStore(filepath.Join(storeDir, "DJOneHub", "notification-preferences.json"))
		var legacyPreferences notification.NotificationPreferences
		if legacy.Read(&legacyPreferences) == nil {
			_ = notificationPreferencesStore.Write(&legacyPreferences)
		}
	}
	legacySMSPath := filepath.Join(storeDir, "DJOneHub", "sms-sent-history.json")
	if err := sms.MigrateLegacySentHistory(database, legacySMSPath); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate sms history: %w", err)
	}
	ops := operation.NewManager(r.Events())
	devices := device.NewService(r)
	smsService := sms.NewService(devices, ops, r, database)
	esimService := esim.NewService(devices, ops, r)
	networkService := network.NewService(devices, ops, r, platformAdapter, database)
	rawATService := rawat.NewService(devices, r)
	firmwareConfig := firmware.ConfigFromEnvironment()
	firmwareConfig.Store = database.Namespace("firmware_settings")
	if detector, ok := platformAdapter.(interface {
		DetectEDL(context.Context) (bool, error)
	}); ok {
		firmwareConfig.DetectEDL = detector.DetectEDL
	}
	firmwareService := firmware.NewService(rawATService, ops, r, firmwareConfig)
	platformDependencies := vowifi.PlatformDependencies{Network: platformAdapter}
	if tunnel, ok := platformAdapter.(transport.PacketTunnel); ok {
		platformDependencies.Tunnel = tunnel
	}
	vowifiService := vowifi.NewService(devices, ops, r, platformDependencies)
	extraService := extras.NewService(devices, ops, r, profileStore, database)
	notificationPreferences := notification.DefaultNotificationPreferences()
	_ = notificationPreferencesStore.Read(&notificationPreferences)
	notificationPreferences = notificationPreferences.Normalize()
	bridge := native.New(nil)
	startupManager := startup.New()
	bridge.SetNotificationPreferences(notificationPreferences)
	notifications := notification.New(notification.Config{
		Events: r.Events(),
		Calls: func(ctx context.Context) ([]notification.CallEvent, error) {
			status := extraService.Calls(ctx)
			out := make([]notification.CallEvent, 0, len(status.History)+1)
			if status.Active != nil {
				out = append(out, extras.CallEventFromRecord(*status.Active))
			}
			for _, record := range status.History {
				out = append(out, extras.CallEventFromRecord(record))
			}
			return out, nil
		},
		SMS: func(ctx context.Context) ([]notification.SMSMessageEvent, error) {
			messages, err := smsService.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]notification.SMSMessageEvent, 0, len(messages))
			for _, message := range messages {
				out = append(out, notification.SMSMessageEvent{Index: message.Index, Sender: message.Sender, Recipient: message.Recipient, Body: message.Body, ReceivedAt: message.ReceivedAt, RecordedAt: message.RecordedAt, ICCID: message.ICCID})
			}
			return out, nil
		},
		Sink: bridge,
	})
	bridge.SetHandler(&nativeCommandHandler{extras: extraService, bridge: bridge})
	app := &App{
		Runtime:           r,
		Operations:        ops,
		Device:            devices,
		SMS:               smsService,
		ESIM:              esimService,
		Network:           networkService,
		RawAT:             rawATService,
		VoWiFi:            vowifiService,
		Extras:            extraService,
		Firmware:          firmwareService,
		Notification:      notifications,
		NativeUI:          bridge,
		Store:             database,
		shutdownAdmission: make(chan struct{}),
	}
	app.HTTP = httpapi.NewServer(httpapi.Config{
		Device:                        devices,
		SMS:                           smsService,
		ESIM:                          esimService,
		Network:                       networkService,
		Notification:                  notifications,
		RawAT:                         rawATService,
		VoWiFi:                        vowifiService,
		Extras:                        extraService,
		Firmware:                      firmwareService,
		Operations:                    ops,
		Runtime:                       r,
		NotificationUIAvailable:       bridge.HasUI,
		NotificationPermissionStatus:  bridge.NotificationPermissionStatus,
		RequestNotificationPermission: bridge.RequestNotificationPermission,
		OpenNotificationSettings:      bridge.OpenNotificationSettings,
		NotificationPreferences:       bridge.NotificationPreferences,
		SetNotificationPreferences: func(preferences notification.NotificationPreferences) error {
			if err := preferences.Validate(); err != nil {
				return err
			}
			if err := notificationPreferencesStore.Write(&preferences); err != nil {
				return err
			}
			bridge.SetNotificationPreferences(preferences)
			return nil
		},
		StartupStatus:     startupManager.Status,
		SetStartupEnabled: startupManager.SetEnabled,
		Admission:         app.admitting,
	})
	return app, nil
}

// admitting reports whether the shutdown admission gate is still open.
func (a *App) admitting() bool {
	select {
	case <-a.shutdownAdmission:
		return false
	default:
		return true
	}
}

// BeginShutdown closes the idempotent shutdown-admission gate shared by the
// HTTP handlers and the operation manager. It must run before the HTTP server
// drains so an already-connected handler cannot start new detached work after
// shutdown begins.
func (a *App) BeginShutdown() {
	a.admissionOnce.Do(func() { close(a.shutdownAdmission) })
}

// nativeCommandHandler executes native UI commands. reject_call reuses the
// extras AT path; open_dashboard answers with the dashboard URL for Swift to
// open with NSWorkspace.
type nativeCommandHandler struct {
	extras *extras.Service
	bridge *native.Bridge
}

func (h *nativeCommandHandler) HandleCommand(ctx context.Context, command notification.Command) {
	switch command.Name {
	case notification.CommandRejectCall:
		callID := command.CallID()
		h.bridge.Send(notification.EventCallRejectStarted, notification.RejectResult{CallID: callID})
		if err := h.extras.Reject(ctx); err != nil {
			h.bridge.Send(notification.EventCallRejectFailed, notification.RejectResult{CallID: callID, Error: err.Error()})
			return
		}
		h.bridge.Send(notification.EventCallRejectSucceeded, notification.RejectResult{CallID: callID})
	case notification.CommandOpenDashboard:
		h.bridge.Send(notification.EventDashboardOpened, notification.DashboardOpened{URL: h.bridge.WebURL()})
	}
}

func (a *App) Start(ctx context.Context) {
	a.Runtime.Start(ctx)
	// SMS and network pollers drive sms.received / network.updated events;
	// extras already starts its own call poller. The notification
	// policy subscribes last so its startup baseline is established after the
	// pollers are running.
	a.SMS.Start(ctx)
	a.Network.Start(ctx)
	a.Extras.Start(ctx)
	if err := a.Notification.Start(ctx); err != nil {
		// A failed policy subscription must not take the device services down.
		_ = err
	}
	// The VoWiFi runtime-event subscription is tied to the session context so
	// it stops when the session ends.
	a.VoWiFi.Start(ctx)
	// The initial runtime scan may publish before the notification subscriber
	// is ready. Send the current snapshot explicitly so the native status item
	// always has a state, including when no device or SIM is present.
	a.NativeUI.UpdateDeviceStatus(a.Runtime.Snapshot())
}

// Stop runs the single bounded shutdown sequence: it closes admission, cancels
// and joins in-flight operations, then stops each background worker in reverse
// of the actual start order (Notification, Extras, Network, SMS, Runtime) and
// waits for each to join before closing storage. A worker that does not stop
// by the deadline returns the context error and leaves the store open, because
// storage is never closed underneath a live writer.
func (a *App) Stop(ctx context.Context) error {
	a.BeginShutdown()
	// Cancel and join in-flight operations first so the resource locks they
	// hold are released before the workers that depend on them stop.
	if err := a.Operations.Shutdown(ctx); err != nil {
		return err
	}
	// The native UI is preserved until the notification sink queue has been
	// drained: the delivery goroutine joins here, and the UI itself is stopped
	// by the caller only after this returns.
	if err := a.Notification.Stop(ctx); err != nil {
		return err
	}
	if err := a.Extras.Stop(ctx); err != nil {
		return err
	}
	if err := a.Network.Stop(ctx); err != nil {
		return err
	}
	// Unregister the SMS inbound consumer before the runtime closes its
	// backend so a stopped service never receives +CMTI notifications.
	if err := a.SMS.Stop(ctx); err != nil {
		return err
	}
	// Stop the VoWiFi session subscription; it started last and depends on the
	// runtime's event bus.
	if err := a.VoWiFi.Stop(ctx); err != nil {
		return err
	}
	a.Runtime.Stop()
	if err := a.Store.Close(); err != nil {
		return err
	}
	return nil
}
