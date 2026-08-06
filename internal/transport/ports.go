package transport

import (
	"context"
	"io"

	"github.com/iniwex5/vohive/internal/domain/device"
)

// DeviceDiscovery 是平台探测契约。运行时是单设备运行时: 它只消费返回列表中
// 的第一个候选 (candidates[0]) 并把它作为受管设备。因此平台实现只须为将被
// 消费的候选做探测工作 (如 AT 端口探测), 不应为其余候选做探测; 探测预算、
// 冷却与回退行为由各平台自持。
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
}

type NetworkTrafficReader interface {
	NetworkTraffic(context.Context, device.Candidate) (rxBytes, txBytes uint64, err error)
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
	Mode               string   `json:"mode,omitempty"`
	NetworkMode        string   `json:"network_mode,omitempty"`
	RadioBand          string   `json:"radio_band,omitempty"`
	Interface          string   `json:"interface,omitempty"`
	Addresses          []string `json:"addresses,omitempty"`
	DefaultRoute       string   `json:"default_route,omitempty"`
	SystemDefaultRoute string   `json:"system_default_route,omitempty"`
	RXBytes            uint64   `json:"rx_bytes"`
	TXBytes            uint64   `json:"tx_bytes"`
}

type Connectivity struct {
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}
