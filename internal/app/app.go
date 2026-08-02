package app

import (
	"context"
	"fmt"
	goruntime "runtime"

	httpapi "github.com/iniwex5/vohive/internal/api/http"
	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/esim"
	"github.com/iniwex5/vohive/internal/application/network"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/rawat"
	"github.com/iniwex5/vohive/internal/application/sms"
	"github.com/iniwex5/vohive/internal/application/vowifi"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/platform/darwin"
	"github.com/iniwex5/vohive/internal/platform/linux"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
	"github.com/iniwex5/vohive/internal/platform/windows"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/transport"
)

type noHardwareDiscovery struct{}

func (noHardwareDiscovery) Discover(context.Context) ([]domain.Candidate, error) { return nil, nil }

type noHardwareBackends struct{}

func (noHardwareBackends) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return nil, "", fmt.Errorf("no modem backend is configured")
}

type App struct {
	Runtime    *runtime.Runtime
	Operations *operation.Manager
	Device     *device.Service
	SMS        *sms.Service
	ESIM       *esim.Service
	Network    *network.Service
	RawAT      *rawat.Service
	VoWiFi     *vowifi.Service
	HTTP       *httpapi.Server
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
	return &App{
		Runtime:    r,
		Operations: ops,
		Device:     devices,
		SMS:        smsService,
		ESIM:       esimService,
		Network:    networkService,
		RawAT:      rawATService,
		VoWiFi:     vowifiService,
		HTTP:       httpapi.NewServer(httpapi.Config{Device: devices, SMS: smsService, ESIM: esimService, Network: networkService, RawAT: rawATService, VoWiFi: vowifiService, Operations: ops, Runtime: r}),
	}, nil
}

func (a *App) Start(ctx context.Context) { a.Runtime.Start(ctx) }
func (a *App) Stop()                     { a.Runtime.Stop() }
