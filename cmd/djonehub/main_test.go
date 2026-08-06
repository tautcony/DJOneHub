package main

import "testing"

func TestValidateListenAddress(t *testing.T) {
	for _, check := range []struct {
		listen string
		port   int
		ok     bool
	}{
		// Accepted loopback forms.
		{"127.0.0.1:7575", 7575, true},
		{"localhost:7575", 7575, true},
		{"[::1]:7575", 7575, true},
		{"127.0.0.1:80", 80, true},
		{"localhost:1", 1, true},
		// Rejected wildcard, non-loopback, and hostname addresses.
		{"0.0.0.0:7575", 0, false},
		{":7575", 0, false},
		{":::7575", 0, false},
		{"192.168.1.1:7575", 0, false},
		{"127.0.0.2:7575", 0, false},
		{"example.com:7575", 0, false},
		{"djonehub.local:7575", 0, false},
		// Invalid or missing ports.
		{"127.0.0.1:0", 0, false},
		{"127.0.0.1:99999", 0, false},
		{"127.0.0.1", 0, false},
		{"127.0.0.1:notaport", 0, false},
		{"", 0, false},
	} {
		port, err := validateListenAddress(check.listen)
		if check.ok && err != nil {
			t.Errorf("validateListenAddress(%q) = %v, want no error", check.listen, err)
			continue
		}
		if !check.ok && err == nil {
			t.Errorf("validateListenAddress(%q) succeeded, want error", check.listen)
			continue
		}
		if check.ok && port != check.port {
			t.Errorf("validateListenAddress(%q) port = %d, want %d", check.listen, port, check.port)
		}
	}
}
