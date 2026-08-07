package driver

import (
	"log/slog"
	"net/http"
	"net/url"
	"testing"
)

func TestNewLoggingRoundTripperUsesHTTPSProxyFromEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:17890")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	roundTripper := NewLoggingRoundTripper(nil, slog.Default())
	requestURL, err := url.Parse("https://smdp.example.com/gsma/rsp2/es9plus/initiateAuthentication")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	proxyURL, err := roundTripper.transport.Proxy(&http.Request{URL: requestURL})
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:17890" {
		t.Fatalf("Proxy() = %v, want http://127.0.0.1:17890", proxyURL)
	}
}
