package backend

import "context"

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

type ESIMPort interface {
	EID(context.Context) (string, error)
	Profiles(context.Context) ([]Profile, error)
	Download(context.Context, string, string, string) error
	Enable(context.Context, string) error
	Rename(context.Context, string, string) error
	Delete(context.Context, string) error
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
