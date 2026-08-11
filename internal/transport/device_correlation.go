package transport

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// Qualcomm EDL 身份的 VID:PID。EDL 端口 (darwin EDLPort) 的查找与进入
// 路径统一引用这两个常量, 避免各适配器间字面量漂移。
const (
	QualcommEDLVendorID  = "05c6"
	QualcommEDLProductID = "9008"
)

// NormalModeIdentities 列出受支持模块的正常模式 USB 身份 ("vid:pid"),
// 是模块变体清单的唯一来源。平台适配器表 (darwin supportedUSBIdentities,
// linux usbProduct) 与 FindOriginal 的复位白名单都必须由此派生。
var NormalModeIdentities = []string{"2ca3:4006", "2c7c:0125"}

// MatchPhysicalDevice selects one candidate with the requested USB identity
// at the original physical location. It rejects missing location data and
// ambiguous observations.
func MatchPhysicalDevice(original device.Candidate, candidates []device.Candidate, vendorID, productID string) (device.Candidate, error) {
	location := strings.TrimSpace(original.Identity.PhysicalLocation)
	if location == "" {
		return device.Candidate{}, identityError("original device has no stable physical location", false, 0)
	}
	vendorID = normalizeUSBID(vendorID)
	productID = normalizeUSBID(productID)
	matches := make([]device.Candidate, 0, 1)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Identity.PhysicalLocation) != location {
			continue
		}
		if normalizeUSBID(candidate.Identity.VendorID) != vendorID || normalizeUSBID(candidate.Identity.ProductID) != productID {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return device.Candidate{}, identityError("matching device was not found at the original physical location", true, 0)
	}
	if len(matches) != 1 {
		return device.Candidate{}, identityError("multiple matching devices were found at the original physical location", false, len(matches))
	}
	return matches[0], nil
}

// MatchUniqueUSBDevice selects one USB identity when no prior physical
// location exists. It rejects ambiguity instead of guessing which device the
// single-device runtime manages.
func MatchUniqueUSBDevice(candidates []device.Candidate, vendorID, productID string) (device.Candidate, error) {
	vendorID = normalizeUSBID(vendorID)
	productID = normalizeUSBID(productID)
	matches := make([]device.Candidate, 0, 1)
	for _, candidate := range candidates {
		if normalizeUSBID(candidate.Identity.VendorID) == vendorID && normalizeUSBID(candidate.Identity.ProductID) == productID {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return device.Candidate{}, identityError("matching USB device was not found", true, 0)
	}
	if len(matches) != 1 {
		return device.Candidate{}, identityError("multiple matching USB devices were found", false, len(matches))
	}
	return matches[0], nil
}

// MatchPhysicalDeviceIdentities matches one of the allow-listed USB
// identities at the original physical location.
func MatchPhysicalDeviceIdentities(original device.Candidate, candidates []device.Candidate, identities ...string) (device.Candidate, error) {
	location := strings.TrimSpace(original.Identity.PhysicalLocation)
	if location == "" {
		return device.Candidate{}, identityError("original device has no stable physical location", false, 0)
	}
	allowed := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		allowed[strings.ToLower(strings.TrimSpace(identity))] = struct{}{}
	}
	matches := make([]device.Candidate, 0, 1)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Identity.PhysicalLocation) != location {
			continue
		}
		if _, ok := allowed[CandidateUSBIdentity(candidate)]; ok {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return device.Candidate{}, identityError("matching device was not found at the original physical location", true, 0)
	}
	if len(matches) != 1 {
		return device.Candidate{}, identityError("multiple matching devices were found at the original physical location", false, len(matches))
	}
	return matches[0], nil
}

func normalizeUSBID(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "0x"), "0X"))
}

func identityError(message string, retryable bool, matches int) error {
	details := map[string]any{"phase": "device_correlation"}
	if matches > 0 {
		details["matches"] = matches
	}
	return derrors.New(derrors.DeviceOffline, message, retryable, details)
}

func CandidateUSBIdentity(candidate device.Candidate) string {
	return fmt.Sprintf("%s:%s", normalizeUSBID(candidate.Identity.VendorID), normalizeUSBID(candidate.Identity.ProductID))
}
