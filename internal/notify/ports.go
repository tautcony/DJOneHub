package notify

import (
	"context"
	"time"
)

// 本文件定义命令层需要的窄端口。notify 不直接依赖 application 包，避免
// application -> notify -> application 的循环依赖；实际绑定在 internal/app
// 里完成（见 app.notifyPorts）。

// DeviceStatus 是 /status 与 /list 需要的设备状态投影。
type DeviceStatus struct {
	State       string
	Healthy     bool
	IMEI        string
	ICCID       string
	IMSI        string
	Firmware    string
	LocalPhone  string
	Operator    string
	NetworkMode string
	Registered  bool
	SIMInserted bool
	SignalDBM   int
	SignalRSRP  int
	SignalRSRQ  int
}

// SMSRecord 是 /sms 列表里的一条短信。
type SMSRecord struct {
	Outbound bool
	Peer     string
	Body     string
	At       time.Time
}

// ESIMProfile 是 /esim 与 /switch 用到的 eSIM 配置文件。
type ESIMProfile struct {
	ICCID    string
	Name     string
	Provider string
	Active   bool
}

// CallSummary 是 /call 展示的通话状态。
type CallSummary struct {
	Active    bool
	Direction string
	State     string
	Number    string
	StartedAt time.Time
}

// Ports 聚合命令层依赖的业务能力。为 nil 的字段表示该能力在当前构建中不可
// 用，对应命令会回复"能力不可用"而不是 panic。
type Ports struct {
	// DeviceStatus 返回当前设备状态。
	DeviceStatus func(ctx context.Context) (DeviceStatus, error)
	// DeviceLabel 返回设备显示名，用于通知抬头。应当廉价且不返回错误。
	DeviceLabel func() string

	// SendSMS 发起短信发送，返回 operation ID。
	SendSMS func(ctx context.Context, recipient, body string) (string, error)
	// ListSMS 返回最近的短信，limit <= 0 表示不限制。
	ListSMS func(ctx context.Context, limit int) ([]SMSRecord, error)

	// ListESIM 返回 eSIM 配置文件列表。
	ListESIM func(ctx context.Context) ([]ESIMProfile, error)
	// EnableESIM 启用指定 ICCID 的配置文件，返回 operation ID。
	EnableESIM func(ctx context.Context, iccid string) (string, error)

	// Calls 返回当前通话状态。
	Calls func(ctx context.Context) (CallSummary, error)
	// Dial 发起呼叫。
	Dial func(ctx context.Context, number string) error
	// Reject 挂断当前通话。
	Reject func(ctx context.Context) error

	// NetworkMode 返回当前联网模式描述。
	NetworkMode func(ctx context.Context) (string, error)
	// SetNetworkMode 切换联网模式，返回 operation ID。
	SetNetworkMode func(ctx context.Context, mode string) (string, error)

	// AwaitOperation 等待异步操作结束，返回是否成功及失败原因。
	AwaitOperation func(ctx context.Context, operationID string) (ok bool, message string)
}
