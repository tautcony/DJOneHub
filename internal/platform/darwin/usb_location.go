package darwin

// composeLibUSBLocationID encodes the macOS/libusb USB path. macOS numbers
// the top-byte bus from zero, so bus 0 is a valid location component.
func composeLibUSBLocationID(bus uint32, ports []uint8) (uint32, bool) {
	if bus > 0xff || len(ports) == 0 || len(ports) > 5 {
		return 0, false
	}
	location := bus << 24
	for index, port := range ports {
		if port == 0 || port > 15 {
			return 0, false
		}
		location |= uint32(port) << uint(20-index*4)
	}
	return location, true
}
