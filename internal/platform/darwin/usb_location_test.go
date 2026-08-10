package darwin

import "testing"

func TestComposeLibUSBLocationIDAllowsZeroBus(t *testing.T) {
	location, ok := composeLibUSBLocationID(0, []uint8{1})
	if !ok || location != 0x100000 {
		t.Fatalf("location=(0x%x, %v), want (0x100000, true)", location, ok)
	}
}

func TestComposeLibUSBLocationIDRejectsInvalidPaths(t *testing.T) {
	for name, ports := range map[string][]uint8{
		"empty":     nil,
		"zero port": {0},
		"wide port": {16},
		"too deep":  {1, 2, 3, 4, 5, 6},
	} {
		t.Run(name, func(t *testing.T) {
			if location, ok := composeLibUSBLocationID(0, ports); ok || location != 0 {
				t.Fatalf("location=(0x%x, %v), want (0, false)", location, ok)
			}
		})
	}
}
