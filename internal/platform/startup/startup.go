package startup

// Status describes whether the current executable can be configured to start
// when the user logs in and whether that configuration is enabled.
type Status struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// Manager owns the platform-specific login startup configuration.
type Manager interface {
	Status() Status
	SetEnabled(bool) error
}
