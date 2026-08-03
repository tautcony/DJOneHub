//go:build !darwin || !cgo

package native

// stubDriver is the no-op UI for platforms without AppKit (or builds without
// cgo). The rest of the application runs headless; Ready() never closes.

type stubDriver struct{}

func newDriver() uiDriver { return stubDriver{} }

func (stubDriver) start(configJSON string, bridge *Bridge) {}

func (stubDriver) handleEvent(eventJSON string) {}

func (stubDriver) stop() {}

func (stubDriver) hasUI() bool { return false }
