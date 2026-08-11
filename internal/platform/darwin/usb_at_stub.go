//go:build !darwin || !cgo || !libusb

package darwin

import (
	"errors"
	"io"
	"time"

	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/transport"
)

type usbDeviceIdentity struct {
	Key, Label, LocationID string
	VendorID, ProductID    uint16
}

func openUSBAT(usbDeviceIdentity) (modem.ATTransport, error) {
	return nil, errors.New("DJI raw USB AT requires a macOS cgo build with the libusb build tag")
}

type unavailableUSBAT struct{}

func (*unavailableUSBAT) Read([]byte) (int, error) { return 0, io.EOF }
func (*unavailableUSBAT) Write([]byte) (int, error) {
	return 0, errors.New("DJI raw USB AT is unavailable in this build")
}
func (*unavailableUSBAT) SetReadTimeout(time.Duration) error { return nil }
func (*unavailableUSBAT) Close() error                       { return nil }

var _ modem.ATTransport = (*unavailableUSBAT)(nil)

func newEDLPort() (transport.EDLPort, bool) { return nil, false }
