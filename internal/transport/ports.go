package transport

import (
	"context"
	"io"

	"github.com/iniwex5/vohive/internal/domain/device"
)

type DeviceDiscovery interface {
	Discover(context.Context) ([]device.Candidate, error)
}

type SerialTransport interface {
	Open(context.Context, device.Candidate) (io.ReadWriteCloser, error)
}

type NetworkController interface {
	Status(context.Context, device.Candidate) (NetworkStatus, error)
	SetMode(context.Context, device.Candidate, string) error
	CheckConnectivity(context.Context, device.Candidate) (Connectivity, error)
}

// NetworkDiagnostics exposes platform-specific inspection without leaking
// operating-system commands into application services.
type NetworkDiagnostics interface {
	Diagnostics(context.Context, device.Candidate) (map[string]any, error)
	CheckRoute(context.Context, device.Candidate, string) (Connectivity, error)
	CellularPolicy(context.Context, device.Candidate) (CellularPolicy, error)
	SetCellularPolicy(context.Context, device.Candidate, bool) (CellularPolicy, error)
}

type PacketTunnel interface {
	Open(context.Context, device.Candidate) (Tunnel, error)
}

type Tunnel interface {
	io.ReadWriteCloser
	Name() string
}

type ServiceInstaller interface {
	Install(context.Context) error
	Uninstall(context.Context) error
}

type NetworkStatus struct {
	Mode         string   `json:"mode,omitempty"`
	NetworkMode  string   `json:"network_mode,omitempty"`
	Interface    string   `json:"interface,omitempty"`
	Addresses    []string `json:"addresses,omitempty"`
	DefaultRoute string   `json:"default_route,omitempty"`
	RXBytes      uint64   `json:"rx_bytes"`
	TXBytes      uint64   `json:"tx_bytes"`
}

type Connectivity struct {
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type CellularPolicy struct {
	ForceOff bool     `json:"force_off"`
	Services []string `json:"services,omitempty"`
}
