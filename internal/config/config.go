package config

import (
	"fmt"
	"strings"
)

const (
	ESIMTransportAT     = "at"
	ESIMTransportQMI    = "qmi"
	ESIMTransportMBIM   = "mbim"
	MBIMTransportAuto   = "auto"
	MBIMTransportProxy  = "proxy"
	MBIMTransportDirect = "direct"
)

func NormalizeESIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", ESIMTransportAT:
		return ESIMTransportAT
	case ESIMTransportQMI:
		return ESIMTransportQMI
	case ESIMTransportMBIM:
		return ESIMTransportMBIM
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func ValidateESIMTransport(in string) error {
	switch NormalizeESIMTransport(in) {
	case ESIMTransportAT, ESIMTransportQMI, ESIMTransportMBIM:
		return nil
	default:
		return fmt.Errorf("invalid esim transport: %q", strings.TrimSpace(in))
	}
}

func NormalizeMBIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", MBIMTransportAuto:
		return MBIMTransportAuto
	case MBIMTransportProxy:
		return MBIMTransportProxy
	case MBIMTransportDirect:
		return MBIMTransportDirect
	default:
		return MBIMTransportAuto
	}
}

// ResolveIPFamily parses DeviceConfig.IPVersion into IPv4/IPv6 enable flags.
// Empty input preserves the legacy IPv4-only behavior.
func ResolveIPFamily(in string) (enableV4 bool, enableV6 bool, err error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", "v4", "ipv4":
		return true, false, nil
	case "v6", "ipv6":
		return false, true, nil
	case "v4v6", "v6v4", "dual", "ipv4v6":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("无效的 ip_version: %q (允许 v4|v6|v4v6)", in)
	}
}

// ESIMSwitchConfig controls deterministic eSIM switch behavior. Zero values preserve current behavior.
type ESIMSwitchConfig struct {
	// UseRefreshTrue uses refresh=true for the main switch path. Default false preserves current behavior.
	UseRefreshTrue bool `mapstructure:"use_refresh_true" yaml:"use_refresh_true"`
	// EventGatedConverge uses UIM indication events to gate post-switch convergence. Default false.
	EventGatedConverge bool `mapstructure:"event_gated_converge" yaml:"event_gated_converge"`
	// RadioCycle performs LowPower -> Online radio cycling around switch. Default false.
	RadioCycle bool `mapstructure:"radio_cycle" yaml:"radio_cycle"`
	// ReinitWindowMS is the expected UIM reinitialization window in milliseconds. Default 0 disables the window.
	// Only effective when EventGatedConverge=true; ReinitWindow marks the period during which GetUIMReadiness
	// timeouts do not trigger whole-core recovery (to avoid triggering on firmware reinitialization stalls).
	// If EventGatedConverge=false, ReinitWindowMS is silently ignored.
	ReinitWindowMS int `mapstructure:"reinit_window_ms" yaml:"reinit_window_ms"`
	// NASAttachTimeoutMS bounds optional attach waiting after Online in milliseconds. Default 0 means do not block.
	NASAttachTimeoutMS int `mapstructure:"nas_attach_timeout_ms" yaml:"nas_attach_timeout_ms"`
}

type DeviceConfig struct {
	ID            string `mapstructure:"id" yaml:"id"`
	Name          string `mapstructure:"name" yaml:"name"` // 设备显示名称
	ModemIMEI     string `mapstructure:"modem_imei" yaml:"modem_imei"`
	USBPath       string `mapstructure:"-" yaml:"-"` // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	ATPort        string `mapstructure:"-" yaml:"-"` // Deprecated: 运行时解析;AT 终端用 Worker.ResolvedATPort()
	ProxyPort     int    `mapstructure:"proxy_port" yaml:"proxy_port"`
	ManagePort    string `mapstructure:"-" yaml:"-"`                           // Deprecated: 运行时解析,绝不从文件读取
	Interface     string `mapstructure:"-" yaml:"-"`                           // Deprecated: 运行时解析,绝不从文件读取
	QMIDevice     string `mapstructure:"-" yaml:"-"`                           // Deprecated: 运行时解析,绝不从文件读取
	ControlDevice string `mapstructure:"-" yaml:"-"`                           // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	MBIMTransport string `mapstructure:"mbim_transport" yaml:"mbim_transport"` // MBIM 传输: auto|proxy|direct，默认 auto
	QMIUseProxy   bool   `mapstructure:"qmi_use_proxy" yaml:"qmi_use_proxy"`   // 是否通过 libqmi qmi-proxy 打开 QMI 控制口
	// 可选：qmi-proxy abstract socket 名称和可执行文件路径。留空使用 quectel-qmi-go 默认值。
	QMIProxyPath       string `mapstructure:"qmi_proxy_path" yaml:"qmi_proxy_path"`
	QMIProxyExecutable string `mapstructure:"qmi_proxy_executable" yaml:"qmi_proxy_executable"`
	ESIMTransport      string `mapstructure:"esim_transport" yaml:"esim_transport"` // eSIM 传输通道: at|qmi|mbim，默认 at
	DeviceBackend      string `mapstructure:"device_backend" yaml:"device_backend"` // 设备后端模式: at|qmi|mbim|auto，默认 at
	USBNetMode         *int   `mapstructure:"usbnet_mode" yaml:"usbnet_mode"`       // 可选：用于校验/设置 Quectel USBNET 模式
	// ESIMSwitch controls deterministic eSIM switch behavior. Zero values preserve current behavior.
	ESIMSwitch ESIMSwitchConfig `mapstructure:"esim_switch" yaml:"esim_switch"`

	OperatorSelectionMode string `mapstructure:"operator_selection_mode" yaml:"operator_selection_mode"`
	OperatorSelectionPLMN string `mapstructure:"operator_selection_plmn" yaml:"operator_selection_plmn"`
	OperatorSelectionRAT  string `mapstructure:"operator_selection_rat" yaml:"operator_selection_rat"`

	// ATTimeoutWatchdogThreshold 连续 AT 超时达到该次数后触发控制面恢复；
	// 0 表示使用默认值 5。长耗时命令（超时超过 30s）不计入连续计数。
	ATTimeoutWatchdogThreshold int `mapstructure:"at_timeout_watchdog_threshold" yaml:"at_timeout_watchdog_threshold"`

	// Serial config
	BaudRate int    `mapstructure:"baud_rate" yaml:"baud_rate"`
	DataBits int    `mapstructure:"data_bits" yaml:"data_bits"`
	StopBits int    `mapstructure:"stop_bits" yaml:"stop_bits"`
	Parity   string `mapstructure:"parity" yaml:"parity"`

	// 以下为运行时有效策略（投影自 card_policies，按 ICCID），不再从配置文件加载
	APN             string `mapstructure:"-" yaml:"-"`
	NetworkEnabled  bool   `mapstructure:"-" yaml:"-"`
	IPVersion       string `mapstructure:"-" yaml:"-"`
	AirplaneEnabled bool   `mapstructure:"-" yaml:"-"`
	SMSEnabled      bool   `mapstructure:"-" yaml:"-"` // SMS 恒开，运行时强制 true

	// USB Audio (自动发现，无需手动配置)
	AudioDevice string `mapstructure:"-" yaml:"-"` // Deprecated: 运行时解析,绝不从文件读取
}
