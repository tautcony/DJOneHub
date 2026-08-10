package transport

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

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
