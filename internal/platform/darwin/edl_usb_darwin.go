//go:build darwin && cgo && libusb

package darwin

/*
#include <libusb-1.0/libusb.h>
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/binary"
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
	if strings.EqualFold(candidate.Identity.VendorID, transport.QualcommEDLVendorID) && strings.EqualFold(candidate.Identity.ProductID, transport.QualcommEDLProductID) {
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
	if strings.TrimSpace(original.Identity.PhysicalLocation) == "" {
		return transport.MatchUniqueUSBDevice(observed, transport.QualcommEDLVendorID, transport.QualcommEDLProductID)
	}
	return transport.MatchPhysicalDevice(original, observed, transport.QualcommEDLVendorID, transport.QualcommEDLProductID)
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
	if strings.EqualFold(original.Identity.VendorID, transport.QualcommEDLVendorID) && strings.EqualFold(original.Identity.ProductID, transport.QualcommEDLProductID) {
		return transport.MatchPhysicalDeviceIdentities(original, candidates, transport.NormalModeIdentities...)
	}
	return transport.MatchPhysicalDevice(original, candidates, original.Identity.VendorID, original.Identity.ProductID)
}

func (p *directEDLPort) ObserveEDL(ctx context.Context, candidate device.Candidate) (device.EDLObservation, error) {
	if !strings.EqualFold(candidate.Identity.VendorID, transport.QualcommEDLVendorID) || !strings.EqualFold(candidate.Identity.ProductID, transport.QualcommEDLProductID) {
		return device.EDLObservation{}, derrors.New(derrors.InvalidRequest, "candidate is not a Qualcomm EDL device", false, nil)
	}
	identity := usbDeviceIdentity{VendorID: 0x05c6, ProductID: 0x9008, LocationID: candidate.Identity.PhysicalLocation}
	var usbCtx *C.libusb_context
	if rc := C.libusb_init(&usbCtx); rc != 0 {
		return device.EDLObservation{}, fmt.Errorf("libusb init: %s", C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_exit(usbCtx)
	handle, _, err := openUSBDevice(usbCtx, identity)
	if err != nil {
		return device.EDLObservation{}, err
	}
	defer C.libusb_close(handle)
	endpointInfo, err := findUSBSaharaCandidate(handle)
	if err != nil {
		return device.EDLObservation{}, err
	}
	if rc := C.libusb_claim_interface(handle, C.int(endpointInfo.iface)); rc != 0 {
		return device.EDLObservation{}, fmt.Errorf("claim Sahara interface %d: %s", endpointInfo.iface, C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_release_interface(handle, C.int(endpointInfo.iface))
	endpoint := &usbDiagEndpoint{handle: handle, endpointIn: endpointInfo.endpointIn, endpointOut: endpointInfo.endpointOut}
	return observeSahara(&usbSaharaEndpoint{ctx: ctx, endpoint: endpoint})
}

type usbDIAGCandidate struct {
	iface       int
	endpointIn  byte
	endpointOut byte
}

type usbSaharaCandidate struct {
	iface       int
	endpointIn  byte
	endpointOut byte
}

func findUSBSaharaCandidate(handle *C.libusb_device_handle) (usbSaharaCandidate, error) {
	deviceHandle := C.libusb_get_device(handle)
	if deviceHandle == nil {
		return usbSaharaCandidate{}, errors.New("USB handle has no device")
	}
	var config *C.struct_libusb_config_descriptor
	if rc := C.libusb_get_active_config_descriptor(deviceHandle, &config); rc != 0 {
		return usbSaharaCandidate{}, fmt.Errorf("get USB config: %s", C.GoString(C.libusb_error_name(rc)))
	}
	defer C.libusb_free_config_descriptor(config)
	interfaces := unsafe.Slice(config._interface, int(config.bNumInterfaces))
	for _, intf := range interfaces {
		for _, alt := range unsafe.Slice(intf.altsetting, int(intf.num_altsetting)) {
			if int(alt.bInterfaceNumber) != 0 {
				continue
			}
			var in, out byte
			for _, endpoint := range unsafe.Slice(alt.endpoint, int(alt.bNumEndpoints)) {
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
				return usbSaharaCandidate{iface: int(alt.bInterfaceNumber), endpointIn: in, endpointOut: out}, nil
			}
		}
	}
	return usbSaharaCandidate{}, errors.New("Qualcomm EDL interface 0 has no bulk Sahara endpoints")
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

type usbSaharaEndpoint struct {
	ctx      context.Context
	endpoint *usbDiagEndpoint
	// leftover 保留一次 bulk 传输中合并到达的后续包字节: macOS libusb 后端
	// 会把排队传输合并进同一次读取, 丢弃尾部会破坏后续包的对齐。
	leftover []byte
}

func (u *usbSaharaEndpoint) WritePacket(packet []byte) error {
	return u.endpoint.Write(u.ctx, packet, 2*time.Second)
}

func (u *usbSaharaEndpoint) readMore(buffer []byte) error {
	count, err := u.endpoint.Read(u.ctx, buffer, 2*time.Second)
	if err != nil {
		return err
	}
	if count <= 0 || count > len(buffer) {
		return fmt.Errorf("Sahara packet read length %d is outside bounds", count)
	}
	u.leftover = append(u.leftover, buffer[:count]...)
	if len(u.leftover) > saharaMaxPacketSize {
		return fmt.Errorf("Sahara packet stream exceeds the maximum packet size")
	}
	return nil
}

func (u *usbSaharaEndpoint) ReadPacket() ([]byte, error) {
	buffer := make([]byte, saharaMaxPacketSize)
	// 短读不是协议错误: 按声明长度收集完整包。
	for len(u.leftover) < 8 {
		if err := u.readMore(buffer); err != nil {
			return nil, err
		}
	}
	if bytes.HasPrefix(bytes.TrimSpace(u.leftover), []byte("<?xml")) {
		packet := append([]byte(nil), u.leftover...)
		u.leftover = nil
		return packet, nil
	}
	declared := binary.LittleEndian.Uint32(u.leftover[4:8])
	if declared < 8 || declared > saharaMaxPacketSize {
		return nil, fmt.Errorf("Sahara packet declared length %d is invalid", declared)
	}
	for len(u.leftover) < int(declared) {
		if err := u.readMore(buffer); err != nil {
			return nil, err
		}
	}
	packet := append([]byte(nil), u.leftover[:declared]...)
	u.leftover = append(u.leftover[:0], u.leftover[declared:]...)
	return packet, nil
}

func (u *usbSaharaEndpoint) ReadData(length int) ([]byte, error) {
	if length <= 0 || length > saharaMaxValueSize {
		return nil, fmt.Errorf("Sahara data length %d is outside bounds", length)
	}
	result := make([]byte, 0, length)
	for len(result) < length {
		buffer := make([]byte, length-len(result))
		count, err := u.endpoint.Read(u.ctx, buffer, 2*time.Second)
		if err != nil {
			return nil, err
		}
		if count <= 0 || count > len(buffer) {
			return nil, fmt.Errorf("Sahara data read length %d is invalid", count)
		}
		result = append(result, buffer[:count]...)
	}
	return result, nil
}

// transferTimeout 把 ctx 的剩余 deadline 折进单次传输超时, 并在 ctx 已取消
// 或到期时立即返回, 使调用方的探测 deadline 与客户端取消真正生效。
func transferTimeout(ctx context.Context, timeout time.Duration) (C.uint, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			if remaining <= 0 {
				return 0, ctx.Err()
			}
			timeout = remaining
		}
	}
	return C.uint(timeout.Milliseconds()), nil
}

func (u *usbDiagEndpoint) Write(ctx context.Context, payload []byte, timeout time.Duration) error {
	if len(payload) == 0 {
		return nil
	}
	ms, err := transferTimeout(ctx, timeout)
	if err != nil {
		return err
	}
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointOut), (*C.uchar)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &transferred, ms)
	if rc != 0 {
		return fmt.Errorf("USB DIAG bulk write: %s", C.GoString(C.libusb_error_name(rc)))
	}
	if int(transferred) != len(payload) {
		return fmt.Errorf("USB DIAG short write: %d/%d", transferred, len(payload))
	}
	return nil
}

func (u *usbDiagEndpoint) Read(ctx context.Context, payload []byte, timeout time.Duration) (int, error) {
	ms, err := transferTimeout(ctx, timeout)
	if err != nil {
		return 0, err
	}
	var transferred C.int
	rc := C.libusb_bulk_transfer(u.handle, C.uchar(u.endpointIn), (*C.uchar)(unsafe.Pointer(&payload[0])), C.int(len(payload)), &transferred, ms)
	if rc == C.LIBUSB_ERROR_TIMEOUT {
		// 因 ctx deadline 而超时是正常的探测截止, 返回 ctx 错误而非协议超时。
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, errUSBTimeout
	}
	if rc != 0 {
		return 0, fmt.Errorf("USB DIAG bulk read: %s", C.GoString(C.libusb_error_name(rc)))
	}
	return int(transferred), nil
}
