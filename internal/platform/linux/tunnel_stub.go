//go:build !linux

package linux

import (
	"fmt"
	"runtime"
)

type unsupportedTunnel struct{}

func openTUN(string) (*unsupportedTunnel, error) {
	return nil, fmt.Errorf("Linux TUN is unavailable on %s", runtime.GOOS)
}

func (*unsupportedTunnel) Read([]byte) (int, error) { return 0, fmt.Errorf("Linux TUN is unavailable") }
func (*unsupportedTunnel) Write([]byte) (int, error) {
	return 0, fmt.Errorf("Linux TUN is unavailable")
}
func (*unsupportedTunnel) Close() error { return nil }
func (*unsupportedTunnel) Name() string { return "" }
