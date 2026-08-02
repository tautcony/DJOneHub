//go:build linux

package linux

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type tunDevice struct {
	file *os.File
	name string
}

func openTUN(name string) (*tunDevice, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\x00") || len(name) >= unix.IFNAMSIZ {
		return nil, fmt.Errorf("invalid TUN interface name %q", name)
	}
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open Linux TUN: %w", err)
	}
	file := os.NewFile(uintptr(fd), "/dev/net/tun")
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open Linux TUN returned nil file")
	}
	request, err := unix.NewIfreq(name)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	request.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(int(file.Fd()), unix.TUNSETIFF, request); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("configure Linux TUN %s: %w", name, err)
	}
	return &tunDevice{file: file, name: request.Name()}, nil
}

func (d *tunDevice) Read(p []byte) (int, error)  { return d.file.Read(p) }
func (d *tunDevice) Write(p []byte) (int, error) { return d.file.Write(p) }
func (d *tunDevice) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	file := d.file
	d.file = nil
	return file.Close()
}
func (d *tunDevice) Name() string {
	if d == nil {
		return ""
	}
	return d.name
}
