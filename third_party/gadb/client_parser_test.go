package gadb

import "testing"

func TestParseDeviceLineWithNoSerialNumber(t *testing.T) {
	serial, attrsIndex, ok := parseDeviceLine("(no serial number) device usb:34603008X transport_id:51")
	if !ok {
		t.Fatal("parseDeviceLine() rejected a no-serial device")
	}
	if serial != "" || attrsIndex != 4 {
		t.Fatalf("parseDeviceLine() = serial %q, attrs index %d; want empty serial and index 4", serial, attrsIndex)
	}
}

func TestParseDeviceLineWithSerialNumber(t *testing.T) {
	serial, attrsIndex, ok := parseDeviceLine("ABC123 device usb:2CA3")
	if !ok {
		t.Fatal("parseDeviceLine() rejected a serialised device")
	}
	if serial != "ABC123" || attrsIndex != 2 {
		t.Fatalf("parseDeviceLine() = serial %q, attrs index %d; want ABC123 and index 2", serial, attrsIndex)
	}
}
