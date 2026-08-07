package main

import "testing"

func TestInspectProxyEnvironmentPrefersUppercaseAndSanitizesCredentials(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://user:secret@proxy.example.com:8080/path")
	t.Setenv("http_proxy", "http://lower.example.com:8080")
	t.Setenv("HTTPS_PROXY", "proxy.example.com:8443")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "localhost,.example.com")
	t.Setenv("no_proxy", "")

	settings, noProxyName := inspectProxyEnvironment()
	if len(settings) != 2 {
		t.Fatalf("len(settings) = %d, want 2", len(settings))
	}
	if settings[0].name != "HTTP_PROXY" || settings[0].endpoint != "http://proxy.example.com:8080" || settings[0].err != nil {
		t.Fatalf("HTTP proxy setting = %+v", settings[0])
	}
	if settings[1].name != "HTTPS_PROXY" || settings[1].endpoint != "http://proxy.example.com:8443" || settings[1].err != nil {
		t.Fatalf("HTTPS proxy setting = %+v", settings[1])
	}
	if noProxyName != "NO_PROXY" {
		t.Fatalf("noProxyName = %q, want NO_PROXY", noProxyName)
	}
}

func TestSanitizedProxyEndpointRejectsInvalidValue(t *testing.T) {
	if _, err := sanitizedProxyEndpoint("://bad proxy"); err == nil {
		t.Fatal("sanitizedProxyEndpoint() error = nil, want invalid proxy URL")
	}
}
