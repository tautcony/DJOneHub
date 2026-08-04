//go:build !darwin

package startup

import "fmt"

type stubManager struct{}

func New() Manager { return stubManager{} }

func (stubManager) Status() Status { return Status{} }

func (stubManager) SetEnabled(bool) error {
	return fmt.Errorf("login startup is unavailable on this platform")
}
