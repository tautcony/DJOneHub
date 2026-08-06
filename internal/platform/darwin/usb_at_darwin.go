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
	var lastErr error
	for _, candidate := range candidates {
		if rc := C.libusb_claim_interface(handle, C.int(candidate.iface)); rc != 0 {
			lastErr = fmt.Errorf("claim USB AT interface %d: %s", candidate.iface, C.GoString(C.libusb_error_name(rc)))
			continue
		}
		dev := &usbAT{ctx: ctx, handle: handle, locationID: location, iface: candidate.iface, endpointIn: candidate.endpointIn, endpointOut: candidate.endpointOut}
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
	if len(matches) > 1 && identity.LocationID != "" {
		if bus, ok := locationBus(identity.LocationID); ok {
			for _, candidate := range matches {
				if int(C.libusb_get_bus_number(candidate)) == bus {
					selected = candidate
					break
				}
			}
		}
	}
	var handle *C.libusb_device_handle
	if rc := C.libusb_open(selected, &handle); rc != 0 {
		return nil, "", fmt.Errorf("open USB device: %s", C.GoString(C.libusb_error_name(rc)))
	}
	return handle, identity.LocationID, nil
}

func locationBus(location string) (int, bool) {
	value := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(location), "0x"), "0X")
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil || parsed>>24 == 0 {
		return 0, false
	}
	return int(parsed >> 24), true
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

// CommandWithPrompt executes an AT command that enters an interactive input
// state, then submits followUp after the modem returns its ">" prompt. This
// is required by AT+CMGS in PDU mode.
func (u *usbAT) CommandWithPrompt(command string, followUp []byte, timeout time.Duration) (string, error) {
	if u == nil || u.handle == nil {
		return "", errors.New("USB AT device is not open")
	}
	command = strings.TrimSpace(command)
	if command == "" || !strings.HasPrefix(strings.ToUpper(command), "AT") {
		return "", errors.New("AT command must start with AT")
	}
	if len(followUp) == 0 {
		return "", errors.New("interactive AT follow-up is empty")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.drainLocked()
	if err := u.bulkWriteLocked([]byte(command+"\r"), timeout); err != nil {
		return "", err
	}

	deadline := time.Now().Add(timeout)
	var response strings.Builder
	promptReceived := false
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
			return normalizeATResponse(response.String()), err
		}
		response.Write(data)
		joined := response.String()
		if !promptReceived {
			if atResponseIsError(joined) {
				return normalizeATResponse(joined), nil
			}
			if !atResponseHasPrompt(joined) {
				continue
			}
			if err := u.bulkWriteLocked(followUp, time.Until(deadline)); err != nil {
				return normalizeATResponse(joined), err
			}
			promptReceived = true
			continue
		}
		if atResponseComplete(joined) {
			return normalizeATResponse(joined), nil
		}
	}

	if promptReceived {
		_ = u.bulkWriteLocked([]byte{0x1b}, 300*time.Millisecond)
	}
	if response.Len() == 0 {
		return "", errors.New("USB interactive AT command timed out without response")
	}
	return normalizeATResponse(response.String()), errors.New("USB interactive AT command timed out before completion")
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

func atResponseHasPrompt(response string) bool {
	trimmed := strings.TrimRight(response, " \t\r\n")
	return strings.HasSuffix(trimmed, ">")
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
