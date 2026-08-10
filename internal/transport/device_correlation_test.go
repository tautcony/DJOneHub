package transport

import (
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
)

func TestMatchPhysicalDeviceFixtures(t *testing.T) {
	original := device.Candidate{Identity: device.Identity{StableID: "synthetic-normal", PhysicalLocation: "port-1", VendorID: "2ca3", ProductID: "4006"}}
	edl := device.Candidate{Identity: device.Identity{StableID: "synthetic-edl", PhysicalLocation: "port-1", VendorID: "05c6", ProductID: "9008"}}
	other := device.Candidate{Identity: device.Identity{StableID: "synthetic-other", PhysicalLocation: "port-2", VendorID: "05c6", ProductID: "9008"}}

	got, err := MatchPhysicalDevice(original, []device.Candidate{other, edl}, "05C6", "9008")
	if err != nil || got.StableID() != edl.StableID() {
		t.Fatalf("match=%+v err=%v", got, err)
	}
	if _, err := MatchPhysicalDevice(original, []device.Candidate{other}, "05c6", "9008"); err == nil {
		t.Fatal("expected a missing-location match error")
	}
	if _, err := MatchPhysicalDevice(device.Candidate{}, []device.Candidate{edl}, "05c6", "9008"); err == nil {
		t.Fatal("expected an original-location error")
	}
	if _, err := MatchPhysicalDevice(original, []device.Candidate{edl, edl}, "05c6", "9008"); err == nil {
		t.Fatal("expected an ambiguous-device error")
	}
}
