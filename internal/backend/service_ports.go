package backend

import "context"

// 编译期断言：所有向设备能力层暴露 eSIM 服务的类型必须完整实现 ESIMPort。
// 能力声明（Capabilities）依赖该接口的类型断言，漏实现任一方法都会导致
// 运行期 eSIM 能力静默消失（capability_not_supported / 422）。
var (
	_ ESIMPort         = (*ATBackend)(nil)
	_ ESIMPort         = (*CommandBackend)(nil)
	_ ESIMPort         = (*BusinessAdapter)(nil)
	_ ESIMStoragePort  = (*ATBackend)(nil)
	_ ESIMStoragePort  = (*CommandBackend)(nil)
	_ ESIMStoragePort  = (*BusinessAdapter)(nil)
	_ ESIMSnapshotPort = (*ATBackend)(nil)
	_ ESIMSnapshotPort = (*CommandBackend)(nil)
	_ NetworkPort      = (*ATBackend)(nil)
	_ NetworkPort      = (*CommandBackend)(nil)
	_ NetworkPort      = (*BusinessAdapter)(nil)
)

type Profile struct {
	ICCID               string `json:"iccid,omitempty"`
	State               string `json:"state,omitempty"`
	StateCode           *int   `json:"state_code,omitempty"`
	StateKnown          bool   `json:"state_known"`
	Label               string `json:"label,omitempty"`
	Phone               string `json:"phone,omitempty"`
	EID                 string `json:"eid,omitempty"`
	AID                 string `json:"aid,omitempty"`
	ServiceProviderName string `json:"service_provider_name,omitempty"`
	ProfileClass        string `json:"profile_class,omitempty"`
}

// ESIMDownloadOptions 携带下载过程中的交互回调。Progress 是单向阶段进度；
// ConfirmationCodeRequest 是双向请求：SM-DP+ 要求确认码时阻塞等待用户输入，
// canceled 为 true 表示用户拒绝或超时，调用方应按取消处理。
type ESIMDownloadOptions struct {
	Progress                func(step string, pct int, msg string)
	ConfirmationCodeRequest func() (code string, canceled bool, err error)
}

// NotificationItem 是 eUICC 待处理通知的展示 DTO。
type NotificationItem struct {
	SequenceNumber int64  `json:"sequence_number"`
	Event          string `json:"event"`
	ICCID          string `json:"iccid,omitempty"`
	Address        string `json:"address,omitempty"`
	CanRetry       bool   `json:"can_retry"`
}

type ESIMPort interface {
	EID(context.Context) (string, error)
	Profiles(context.Context) ([]Profile, error)
	Download(context.Context, string, string, string, *ESIMDownloadOptions) error
	Enable(context.Context, string) error
	Disable(context.Context, string) error
	Rename(context.Context, string, string) error
	Delete(context.Context, string) error
	ListNotifications(context.Context) ([]NotificationItem, error)
	ProcessNotification(context.Context, int64) error
	RemoveNotification(context.Context, int64) error
}

// ESIMStorageInfo reports the writable storage remaining on the eUICC. It is
// optional so backends without an EUICCInfo implementation retain eSIM support.
type ESIMStorageInfo struct {
	FreeNvramBytes int32  `json:"free_nvram_bytes"`
	FreeNvram      string `json:"free_nvram"`
}

type ESIMStoragePort interface {
	ESIMStorage(context.Context) (ESIMStorageInfo, error)
}

// ESIMDeviceInfo is the product identity reported by an eUICC vendor applet.
// It is optional because standard eUICCs do not necessarily expose it.
type ESIMDeviceInfo struct {
	SKU          string `json:"sku_name,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
}

type ESIMDeviceInfoPort interface {
	ESIMDeviceInfo(context.Context) (ESIMDeviceInfo, error)
}

type ESIMSnapshot struct {
	EID        string
	Profiles   []Profile
	DeviceInfo ESIMDeviceInfo
	Storage    ESIMStorageInfo
}

type ESIMSnapshotPort interface {
	ESIMSnapshot(context.Context) (ESIMSnapshot, error)
}

type NetworkPort interface {
	Status(context.Context) (map[string]any, error)
	SetMode(context.Context, string) error
	Traffic(context.Context) (map[string]any, error)
	Check(context.Context) (map[string]any, error)
}

type VoWiFiPort interface {
	Enable(context.Context) error
	Disable(context.Context) error
	Reconnect(context.Context) error
	Status(context.Context) (map[string]any, error)
}

// VoWiFiServicePort avoids the Enable/Disable method names used by ESIMPort.
// Adapters that expose both services can implement this named contract without
// an ambiguous Go method set.
type VoWiFiServicePort interface {
	EnableVoWiFi(context.Context) error
	DisableVoWiFi(context.Context) error
	ReconnectVoWiFi(context.Context) error
	VoWiFiStatus(context.Context) (map[string]any, error)
}
