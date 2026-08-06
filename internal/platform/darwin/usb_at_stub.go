//go:build !darwin || !cgo || !libusb

package darwin

import (
	"errors"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
)

type usbDeviceIdentity struct {
	Key, Label, LocationID string
	VendorID, ProductID    uint16
}

func openUSBAT(usbDeviceIdentity) (backend.ATCommandTransport, error) {
	return nil, errors.New("DJI raw USB AT requires a macOS cgo build with the libusb build tag")
}

type unavailableUSBAT struct{}

func (*unavailableUSBAT) Command(string, time.Duration) (string, error) {
	return "", errors.New("DJI raw USB AT is unavailable in this build")
}
func (*unavailableUSBAT) Close() error { return nil }
