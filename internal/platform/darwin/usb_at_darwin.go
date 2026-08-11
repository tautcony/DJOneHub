//go:build darwin && cgo && libusb

package darwin

/*
#cgo darwin,libusb LDFLAGS: -lusb-1.0
#include <libusb-1.0/libusb.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/iniwex5/vohive/internal/modem"
)

type usbDeviceIdentity struct {
	Key, Label, LocationID string
	VendorID, ProductID    uint16
}

type usbAT struct {
	ctx         *C.libusb_context
	handle      *C.libusb_device_handle
	locationID  string
	iface       int
	endpointIn  byte
	endpointOut byte
	mu          sync.Mutex
	readTimeout time.Duration
}

type usbATCandidate struct {
	iface       int
	endpointIn  byte
	endpointOut byte
}

func openUSBAT(identity usbDeviceIdentity) (*usbAT, error) {
	var ctx *C.libusb_context
	if rc := C.libusb_init(&ctx); rc != 0 {
		return nil, fmt.Errorf("libusb init: %s", C.GoString(C.libusb_error_name(rc)))
	}
	handle, location, err := openUSBDevice(ctx, identity)
	if err != nil {
		C.libusb_exit(ctx)
		return nil, err
	}
	candidates, err := usbATCandidates(handle)
	if err != nil {
		C.libusb_close(handle)
		C.libusb_exit(ctx)
		return nil, err
	}
	allowedInterface, allowed := map[string]int{"dji": 2, "quectel": 2}[identity.Key]
	if !allowed {
		C.libusb_close(handle)
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("USB AT interface is not allow-listed for %s", identity.Key)
	}
	var lastErr error
	for _, candidate := range candidates {
		if candidate.iface != allowedInterface {
			continue
		}
		if rc := C.libusb_claim_interface(handle, C.int(candidate.iface)); rc != 0 {
			lastErr = fmt.Errorf("claim USB AT interface %d: %s", candidate.iface, C.GoString(C.libusb_error_name(rc)))
			continue
		}
		dev := &usbAT{ctx: ctx, handle: handle, locationID: location, iface: candidate.iface, endpointIn: candidate.endpointIn, endpointOut: candidate.endpointOut, readTimeout: 100 * time.Millisecond}
		if response, probeErr := dev.Command("AT", 900*time.Millisecond); probeErr == nil && atProbeSucceeded(response) {
			return dev, nil
		} else if probeErr != nil {
			lastErr = probeErr
		} else {
			lastErr = fmt.Errorf("unexpected AT probe response %q", response)
		}
		C.libusb_release_interface(handle, C.int(candidate.iface))
	}
	C.libusb_close(handle)
	C.libusb_exit(ctx)
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no USB bulk AT interface found")
}

var _ modem.ATTransport = (*usbAT)(nil)

// Read implements the stream transport consumed by modem.Manager. The short
// timeout lets the manager observe shutdown and continue its command loop when
// the USB endpoint has no data.
func (u *usbAT) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.handle == nil {
		return 0, errors.New("USB AT device is not open")
	}
	timeout := u.readTimeout
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	data, err := u.bulkReadLocked(timeout)
	if err != nil {
		return 0, err
	}
	return copy(buffer, data), nil
}

// Write implements the stream transport consumed by modem.Manager.
func (u *usbAT) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.handle == nil {
		return 0, errors.New("USB AT device is not open")
	}
	if err := u.bulkWriteLocked(payload, 3*time.Second); err != nil {
		return 0, err
	}
	return len(payload), nil
}

// SetReadTimeout lets the shared manager tune the polling interval without
// depending on libusb types.
func (u *usbAT) SetReadTimeout(timeout time.Duration) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.handle == nil {
		return errors.New("USB AT device is not open")
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	u.readTimeout = timeout
	return nil
}

func openUSBDevice(ctx *C.libusb_context, identity usbDeviceIdentity) (*C.libusb_device_handle, string, error) {
	var list **C.libusb_device
	count := C.libusb_get_device_list(ctx, &list)
	if count < 0 {
		return nil, "", fmt.Errorf("list USB devices: %s", C.GoString(C.libusb_error_name(C.int(count))))
	}
	defer C.libusb_free_device_list(list, 1)
	devices := unsafe.Slice(list, int(count))
	var matches []*C.libusb_device
	for _, device := range devices {
		var descriptor C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(device, &descriptor) != 0 {
			continue
		}
		if uint16(descriptor.idVendor) == identity.VendorID && uint16(descriptor.idProduct) == identity.ProductID {
			matches = append(matches, device)
		}
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("USB device %04x:%04x not found", identity.VendorID, identity.ProductID)
	}
	selected := matches[0]
	if identity.LocationID != "" {
		locationID, ok := parseLocationID(identity.LocationID)
		if !ok {
			return nil, "", fmt.Errorf("USB physical location %q cannot be matched", identity.LocationID)
		}
		located := make([]*C.libusb_device, 0, 1)
		for _, candidate := range matches {
			if candidateLocation, locationOK := libusbLocationID(candidate); locationOK && candidateLocation == locationID {
				located = append(located, candidate)
			}
		}
		if len(located) == 0 {
			return nil, "", fmt.Errorf("USB device %04x:%04x not found at location %s", identity.VendorID, identity.ProductID, identity.LocationID)
		}
		if len(located) > 1 {
			return nil, "", fmt.Errorf("multiple USB devices %04x:%04x match location %s", identity.VendorID, identity.ProductID, identity.LocationID)
		}
		selected = located[0]
	} else if len(matches) != 1 {
		return nil, "", fmt.Errorf("multiple USB devices %04x:%04x found without a physical location", identity.VendorID, identity.ProductID)
	}
	var handle *C.libusb_device_handle
	if rc := C.libusb_open(selected, &handle); rc != 0 {
		return nil, "", fmt.Errorf("open USB device: %s", C.GoString(C.libusb_error_name(rc)))
	}
	return handle, identity.LocationID, nil
}

func locationBus(location string) (int, bool) {
	parsed, ok := parseLocationID(location)
	if !ok || parsed>>24 == 0 {
		return 0, false
	}
	return int(parsed >> 24), true
}

func parseLocationID(location string) (uint32, bool) {
	value := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(location), "0x"), "0X")
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

func libusbLocationID(device *C.libusb_device) (uint32, bool) {
	bus := uint32(C.libusb_get_bus_number(device))
	ports := make([]C.uint8_t, 7)
	count := int(C.libusb_get_port_numbers(device, &ports[0], C.int(len(ports))))
	if count <= 0 || count > 5 {
		return 0, false
	}
	path := make([]uint8, count)
	for index := 0; index < count; index++ {
		path[index] = uint8(ports[index])
	}
	return composeLibUSBLocationID(bus, path)
}

func usbATCandidates(handle *C.libusb_device_handle) ([]usbATCandidate, error) {
	device := C.libusb_get_device(handle)
	if device == nil {
		return nil, errors.New("USB handle has no device")
	}
	var config *C.struct_libusb_config_descriptor
	if rc := C.libusb_get_active_config_descriptor(device, &config); rc != 0 {
		return nil, fmt.Errorf("get USB config: %s", C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_free_config_descriptor(config)
	var result []usbATCandidate
	interfaces := unsafe.Slice(config._interface, int(config.bNumInterfaces))
	for _, intf := range interfaces {
		altsettings := unsafe.Slice(intf.altsetting, int(intf.num_altsetting))
		for _, alt := range altsettings {
			var endpointIn, endpointOut byte
			endpoints := unsafe.Slice(alt.endpoint, int(alt.bNumEndpoints))
			for _, endpoint := range endpoints {
				if byte(endpoint.bmAttributes)&byte(C.LIBUSB_TRANSFER_TYPE_MASK) != byte(C.LIBUSB_TRANSFER_TYPE_BULK) {
					continue
				}
				address := byte(endpoint.bEndpointAddress)
				if address&byte(C.LIBUSB_ENDPOINT_IN) != 0 {
					endpointIn = address
				} else {
					endpointOut = address
				}
			}
			if endpointIn != 0 && endpointOut != 0 {
				result = append(result, usbATCandidate{iface: int(alt.bInterfaceNumber), endpointIn: endpointIn, endpointOut: endpointOut})
			}
		}
	}
	return result, nil
}

func (u *usbAT) Command(command string, timeout time.Duration) (string, error) {
	if u == nil || u.handle == nil {
		return "", errors.New("USB AT device is not open")
	}
	command = strings.TrimSpace(command)
	if command == "" || !strings.HasPrefix(strings.ToUpper(command), "AT") {
		return "", errors.New("AT command must start with AT")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.drainLocked()
	if err := u.bulkWriteLocked([]byte(command+"\r"), timeout); err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	var response strings.Builder
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 900*time.Millisecond {
			remaining = 900 * time.Millisecond
		}
		data, err := u.bulkReadLocked(remaining)
		if errors.Is(err, errUSBTimeout) {
			continue
		}
		if err != nil {
			return response.String(), err
		}
		response.Write(data)
		if atResponseComplete(response.String()) {
			return normalizeATResponse(response.String()), nil
		}
	}
	if response.Len() == 0 {
		return "", errors.New("USB AT command timed out without response")
	}
	return normalizeATResponse(response.String()), nil
}

func (u *usbAT) Close() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.handle != nil {
		C.libusb_release_interface(u.handle, C.int(u.iface))
		C.libusb_close(u.handle)
		u.handle = nil
	}
	if u.ctx != nil {
		C.libusb_exit(u.ctx)
		u.ctx = nil
	}
	return nil
}

var errUSBTimeout = errors.New("USB timeout")

func (u *usbAT) drainLocked() {
	for {
		if _, err := u.bulkReadLocked(80 * time.Millisecond); err != nil {
			return
		}
	}
}

func (u *usbAT) bulkWriteLocked(payload []byte, timeout time.Duration) error {
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointOut), (*C.uchar)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &transferred, C.uint(timeout.Milliseconds()))
	if rc != 0 {
		return fmt.Errorf("USB bulk write: %s", C.GoString(C.libusb_error_name(rc)))
	}
	if int(transferred) != len(payload) {
		return fmt.Errorf("USB bulk write short transfer: %d/%d", int(transferred), len(payload))
	}
	return nil
}

func (u *usbAT) bulkReadLocked(timeout time.Duration) ([]byte, error) {
	buffer := make([]byte, 512)
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointIn), (*C.uchar)(unsafe.Pointer(&buffer[0])), C.int(len(buffer)), &transferred, C.uint(timeout.Milliseconds()))
	if rc == C.LIBUSB_ERROR_TIMEOUT {
		return nil, errUSBTimeout
	}
	if rc != 0 {
		return nil, fmt.Errorf("USB bulk read: %s", C.GoString(C.libusb_error_name(rc)))
	}
	return buffer[:int(transferred)], nil
}

func atResponseComplete(response string) bool {
	normalized := strings.ReplaceAll(response, "\r\n", "\n")
	return strings.Contains(normalized, "\nOK\n") || strings.HasSuffix(normalized, "\nOK") || atResponseIsError(normalized)
}

func atResponseIsError(response string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(response, "\r\n", "\n"))
	return strings.Contains(normalized, "\nERROR\n") || strings.HasSuffix(normalized, "\nERROR") || strings.Contains(normalized, "+CME ERROR:") || strings.Contains(normalized, "+CMS ERROR:")
}

func atProbeSucceeded(response string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(response), "\r\n", "\n")
	return normalized == "OK" || strings.HasSuffix(normalized, "\nOK")
}

func normalizeATResponse(response string) string {
	response = strings.ReplaceAll(response, "\r\n", "\n")
	response = strings.ReplaceAll(response, "\r", "\n")
	return strings.TrimSpace(response)
}
