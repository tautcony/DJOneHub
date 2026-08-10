package unsupported

import (
	"context"
	"io"

	"github.com/iniwex5/vohive/internal/domain/device"
	domainErrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/transport"
)

type Adapter struct {
	Name         string
	Capabilities device.CapabilitySet
	Paths        Paths
}

func New(name string, capabilities device.CapabilitySet) *Adapter {
	return &Adapter{Name: name, Capabilities: capabilities.Clone(), Paths: PathsFor(name)}
}
func (a *Adapter) Discover(context.Context) ([]device.Candidate, error) {
	return nil, unsupported("device_status", "discover")
}
func (a *Adapter) Open(context.Context, device.Candidate) (io.ReadWriteCloser, error) {
	return nil, unsupported("serial_transport", "serial_open")
}
func (a *Adapter) Status(context.Context, device.Candidate) (transport.NetworkStatus, error) {
	return transport.NetworkStatus{}, unsupported("network_status", "network_status")
}
func (a *Adapter) SetMode(context.Context, device.Candidate, string) error {
	return unsupported("network_control", "network_set_mode")
}
func (a *Adapter) CheckConnectivity(context.Context, device.Candidate) (transport.Connectivity, error) {
	return transport.Connectivity{}, unsupported("network_status", "network_check")
}
func (a *Adapter) Tunnel(context.Context, device.Candidate) (transport.Tunnel, error) {
	return nil, unsupported("packet_tunnel", "packet_tunnel")
}
func (a *Adapter) Install(context.Context) error {
	return unsupported("service_install", "service_install")
}
func (a *Adapter) Uninstall(context.Context) error {
	return unsupported("service_install", "service_uninstall")
}

func unsupported(capability, operation string) error {
	return domainErrors.CapabilityMissing(capability, operation, "the platform adapter has not verified this operation")
}

// Unsupported returns the standard structured capability error for optional
// platform ports implemented as stubs.
func Unsupported(capability, operation string) error { return unsupported(capability, operation) }

type Paths struct {
	Log         string
	Config      string
	Data        string
	Permissions string
}

func PathsFor(platform string) Paths {
	switch platform {
	case "linux":
		return Paths{Log: "/var/log/djonehub", Config: "/etc/djonehub", Data: "/var/lib/djonehub", Permissions: "service user requires serial and modem group access"}
	case "darwin":
		return Paths{Log: "~/Library/Logs/DJOneHub", Config: "~/Library/Application Support/DJOneHub", Data: "~/Library/Application Support/DJOneHub", Permissions: "user approval may be required for USB/network extensions"}
	case "windows":
		return Paths{Log: "%ProgramData%\\DJOneHub\\logs", Config: "%ProgramData%\\DJOneHub\\config", Data: "%ProgramData%\\DJOneHub\\data", Permissions: "service account requires COM/USB and network permissions"}
	default:
		return Paths{Log: "./logs", Config: "./config", Data: "./data", Permissions: "local process permissions"}
	}
}
