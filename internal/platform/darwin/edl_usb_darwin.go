//go:build darwin && cgo && libusb

package darwin

/*
#cgo darwin,libusb LDFLAGS: -lusb-1.0
#include <libusb-1.0/libusb.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/transport"
)

type directEDLPort struct{}

func newEDLPort() (transport.EDLPort, bool) { return &directEDLPort{}, true }

func (p *directEDLPort) EnterEDL(ctx context.Context, candidate device.Candidate) error {
	if strings.EqualFold(candidate.Identity.VendorID, "05c6") && strings.EqualFold(candidate.Identity.ProductID, "9008") {
		return nil
	}
	identity := usbDeviceIdentityForCandidate(candidate)
	if identity.VendorID == 0 || identity.ProductID == 0 {
		return derrors.New(derrors.InvalidRequest, "the selected device has no supported USB identity", false, nil)
	}
	var usbCtx *C.libusb_context
	if rc := C.libusb_init(&usbCtx); rc != 0 {
		return fmt.Errorf("libusb init: %s", C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_exit(usbCtx)
	handle, _, err := openUSBDevice(usbCtx, identity)
	if err != nil {
		return err
	}
	defer C.libusb_close(handle)
	candidateInfo, err := findUSBDIAGCandidate(handle, identity.Key)
	if err != nil {
		return err
	}
	if rc := C.libusb_claim_interface(handle, C.int(candidateInfo.iface)); rc != 0 {
		return fmt.Errorf("claim DIAG interface %d: %s", candidateInfo.iface, C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_release_interface(handle, C.int(candidateInfo.iface))
	endpoint := &usbDiagEndpoint{handle: handle, endpointIn: candidateInfo.endpointIn, endpointOut: candidateInfo.endpointOut}
	return runDIAGSwitch(ctx, endpoint, 3*time.Second)
}

func (p *directEDLPort) FindEDL(ctx context.Context, original device.Candidate) (device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return device.Candidate{}, err
	}
	observed := discoverEDLIdentities(ctx)
	return transport.MatchPhysicalDevice(original, observed, "05c6", "9008")
}

func (p *directEDLPort) FindOriginal(ctx context.Context, original device.Candidate) (device.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return device.Candidate{}, err
	}
	observed := discoverUSBIdentities(ctx)
	candidates := make([]device.Candidate, 0, len(observed))
	for _, item := range observed {
		candidates = append(candidates, device.Candidate{Identity: device.Identity{
			StableID: "usb/" + item.key + "/" + item.location, PhysicalLocation: item.location,
			VendorID: fmt.Sprintf("%04x", item.vendorID), ProductID: fmt.Sprintf("%04x", item.productID),
			Manufacturer: item.vendor, Product: item.product,
		}})
	}
	return transport.MatchPhysicalDevice(original, candidates, original.Identity.VendorID, original.Identity.ProductID)
}

type usbDIAGCandidate struct {
	iface       int
	endpointIn  byte
	endpointOut byte
}

func findUSBDIAGCandidate(handle *C.libusb_device_handle, key string) (usbDIAGCandidate, error) {
	deviceHandle := C.libusb_get_device(handle)
	if deviceHandle == nil {
		return usbDIAGCandidate{}, errors.New("USB handle has no device")
	}
	var config *C.struct_libusb_config_descriptor
	if rc := C.libusb_get_active_config_descriptor(deviceHandle, &config); rc != 0 {
		return usbDIAGCandidate{}, fmt.Errorf("get USB config: %s", C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_free_config_descriptor(config)
	allowInterface := map[string]int{"dji": 0, "quectel": 0}
	iface, allowed := allowInterface[key]
	if !allowed {
		return usbDIAGCandidate{}, fmt.Errorf("DIAG interface is not allow-listed for %s", key)
	}
	interfaces := unsafe.Slice(config._interface, int(config.bNumInterfaces))
	for _, intf := range interfaces {
		altsettings := unsafe.Slice(intf.altsetting, int(intf.num_altsetting))
		for _, alt := range altsettings {
			if int(alt.bInterfaceNumber) != iface {
				continue
			}
			var in, out byte
			endpoints := unsafe.Slice(alt.endpoint, int(alt.bNumEndpoints))
			for _, endpoint := range endpoints {
				if byte(endpoint.bmAttributes)&byte(C.LIBUSB_TRANSFER_TYPE_MASK) != byte(C.LIBUSB_TRANSFER_TYPE_BULK) {
					continue
				}
				address := byte(endpoint.bEndpointAddress)
				if address&byte(C.LIBUSB_ENDPOINT_IN) != 0 {
					in = address
				} else {
					out = address
				}
			}
			if in != 0 && out != 0 {
				return usbDIAGCandidate{iface: iface, endpointIn: in, endpointOut: out}, nil
			}
		}
	}
	return usbDIAGCandidate{}, fmt.Errorf("allow-listed DIAG interface %d has no bulk IN/OUT endpoints", iface)
}

type usbDiagEndpoint struct {
	handle      *C.libusb_device_handle
	endpointIn  byte
	endpointOut byte
}

func (u *usbDiagEndpoint) Write(_ context.Context, payload []byte, timeout time.Duration) error {
	if len(payload) == 0 {
		return nil
	}
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointOut), (*C.uchar)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &transferred, C.uint(timeout.Milliseconds()))
	if rc != 0 {
		return fmt.Errorf("USB DIAG bulk write: %s", C.GoString(C.libusb_error_name(rc)))
	}
	if int(transferred) != len(payload) {
		return fmt.Errorf("USB DIAG short write: %d/%d", transferred, len(payload))
	}
	return nil
}

func (u *usbDiagEndpoint) Read(_ context.Context, payload []byte, timeout time.Duration) (int, error) {
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointIn), (*C.uchar)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &transferred, C.uint(timeout.Milliseconds()))
	if rc == C.LIBUSB_ERROR_TIMEOUT {
		return 0, errUSBTimeout
	}
	if rc != 0 {
		return 0, fmt.Errorf("USB DIAG bulk read: %s", C.GoString(C.libusb_error_name(rc)))
	}
	return int(transferred), nil
}
