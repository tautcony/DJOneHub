package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	httpapi "github.com/iniwex5/vohive/internal/api/http"
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
	"github.com/iniwex5/vohive/internal/platform/darwin"
	"github.com/iniwex5/vohive/internal/platform/darwin/native"
	"github.com/iniwex5/vohive/internal/platform/linux"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/platform/windows"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/transport"
)

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
	Notification *notification.Service
	NativeUI     *native.Bridge
	HTTP         *httpapi.Server
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
		discovery, backends, networkAdapter = adapter, backend.NewATFactory(nil), adapter
	case "windows":
		adapter := windows.New()
		discovery, backends, networkAdapter = adapter, backend.NewATFactory(nil), adapter
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
	ops := operation.NewManager(r.Events())
	devices := device.NewService(r)
	smsService := sms.NewService(devices, ops, r)
	esimService := esim.NewService(devices, ops, r)
	networkService := network.NewService(devices, ops, r, platformAdapter)
	rawATService := rawat.NewService(devices, r)
	platformDependencies := vowifi.PlatformDependencies{Network: platformAdapter}
	if tunnel, ok := platformAdapter.(transport.PacketTunnel); ok {
		platformDependencies.Tunnel = tunnel
	}
	vowifiService := vowifi.NewService(devices, ops, r, platformDependencies)
	storeDir, _ := os.UserConfigDir()
	if storeDir == "" {
		storeDir = "."
	}
	extraService := extras.NewService(devices, ops, r, filepath.Join(storeDir, "DJOneHub", "profile-notes.json"))
	notificationPreferencesStore := storage.NewJSONStore(filepath.Join(storeDir, "DJOneHub", "notification-preferences.json"))
	notificationPreferences := notification.DefaultNotificationPreferences()
	_ = notificationPreferencesStore.Read(&notificationPreferences)
	notificationPreferences = notificationPreferences.Normalize()
	bridge := native.New(nil)
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
				out = append(out, notification.SMSMessageEvent{Index: message.Index, Sender: message.Sender, Recipient: message.Recipient, Body: message.Body, Code: message.Code, ReceivedAt: message.ReceivedAt})
			}
			return out, nil
		},
		Sink: bridge,
	})
	bridge.SetHandler(&nativeCommandHandler{extras: extraService, bridge: bridge})
	return &App{
		Runtime:      r,
		Operations:   ops,
		Device:       devices,
		SMS:          smsService,
		ESIM:         esimService,
		Network:      networkService,
		RawAT:        rawATService,
		VoWiFi:       vowifiService,
		Extras:       extraService,
		Notification: notifications,
		NativeUI:     bridge,
		HTTP: httpapi.NewServer(httpapi.Config{
			Device:                        devices,
			SMS:                           smsService,
			ESIM:                          esimService,
			Network:                       networkService,
			Notification:                  notifications,
			RawAT:                         rawATService,
			VoWiFi:                        vowifiService,
			Extras:                        extraService,
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
		}),
	}, nil
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
	// extras already starts its own call and GPS pollers. The notification
	// policy subscribes last so its startup baseline is established after the
	// pollers are running.
	a.SMS.Start(ctx)
	a.Network.Start(ctx)
	a.Extras.Start(ctx)
	if err := a.Notification.Start(ctx); err != nil {
		// A failed policy subscription must not take the device services down.
		_ = err
	}
}

func (a *App) Stop() {
	a.Notification.Stop()
	a.Runtime.Stop()
}
