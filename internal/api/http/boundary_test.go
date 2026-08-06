package httpapi

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "localhost": true, "::1": true,
		"[127.0.0.1]": true, "[::1]": true,
		"0.0.0.0": false, "::": false, "127.0.0.2": false, "192.168.1.1": false,
		"example.com": false, "djonehub": false, "": false,
	} {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLoopbackHostPort(t *testing.T) {
	for _, check := range []struct {
		hostport string
		port     int
		want     bool
	}{
		{"127.0.0.1:7575", 7575, true},
		{"localhost:7575", 7575, true},
		{"[::1]:7575", 7575, true},
		{"127.0.0.1:7575", 7576, false},
		{"localhost:7575", 80, false},
		{"[::1]:7575", 7575, true},
		{"127.0.0.1", 7575, false},
		{"127.0.0.1:", 7575, false},
		{"example.com:7575", 7575, false},
		{"0.0.0.0:7575", 7575, false},
		{"127.0.0.1:7575", 0, false},
	} {
		if got := loopbackHostPort(check.hostport, check.port); got != check.want {
			t.Errorf("loopbackHostPort(%q, %d) = %v, want %v", check.hostport, check.port, got, check.want)
		}
	}
}

func TestLoopbackOriginAllowed(t *testing.T) {
	server := NewServer(Config{LoopbackPort: 7575})
	for _, check := range []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "ipv4 same origin", host: "127.0.0.1:7575", origin: "http://127.0.0.1:7575", want: true},
		{name: "localhost same origin", host: "localhost:7575", origin: "http://localhost:7575", want: true},
		{name: "ipv6 same origin", host: "[::1]:7575", origin: "http://[::1]:7575", want: true},
		{name: "https same origin", host: "127.0.0.1:7575", origin: "https://127.0.0.1:7575", want: true},
		{name: "missing origin accepted", host: "127.0.0.1:7575", origin: "", want: true},
		{name: "missing origin on non-bound port", host: "127.0.0.1:5176", origin: "", want: false},
		{name: "evil origin", host: "127.0.0.1:7575", origin: "http://evil.com", want: false},
		{name: "wrong scheme", host: "127.0.0.1:7575", origin: "ftp://127.0.0.1:7575", want: false},
		{name: "wrong origin port", host: "127.0.0.1:7575", origin: "http://127.0.0.1:8080", want: false},
		{name: "origin without port", host: "127.0.0.1:7575", origin: "http://127.0.0.1", want: false},
		{name: "malformed origin", host: "127.0.0.1:7575", origin: "::not-a-url::", want: false},
		{name: "non-loopback host header", host: "example.com:7575", origin: "http://127.0.0.1:7575", want: false},
		{name: "tunneled host port with bound origin", host: "127.0.0.1:9999", origin: "http://127.0.0.1:7575", want: true},
		{name: "localhost page against ipv4 host", host: "127.0.0.1:7575", origin: "http://localhost:7575", want: true},
		{name: "dev proxy origin matches host port", host: "127.0.0.1:5176", origin: "http://localhost:5176", want: true},
		{name: "dev proxy ipv6 origin matches host port", host: "[::1]:5176", origin: "http://[::1]:5176", want: true},
		{name: "dev proxy origin port mismatch", host: "127.0.0.1:5176", origin: "http://127.0.0.1:8080", want: false},
	} {
		t.Run(check.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+check.host+"/api/v1/events/ws", nil)
			if check.origin != "" {
				request.Header.Set("Origin", check.origin)
			}
			if got := server.loopbackOriginAllowed(request); got != check.want {
				t.Errorf("loopbackOriginAllowed(host=%q origin=%q) = %v, want %v", check.host, check.origin, got, check.want)
			}
		})
	}
}

func TestADBShellUpgraderEnforcesLoopbackOrigin(t *testing.T) {
	server := NewServer(Config{LoopbackPort: 7575})
	checkOrigin := server.adbShellUpgrader().CheckOrigin
	if checkOrigin == nil {
		t.Fatal("adb shell upgrader has no CheckOrigin")
	}
	allowed := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7575/api/v1/firmware/actions/adb/shell/ws", nil)
	allowed.Header.Set("Origin", "http://127.0.0.1:7575")
	if !checkOrigin(allowed) {
		t.Fatal("same-origin upgrade was rejected")
	}
	rejected := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7575/api/v1/firmware/actions/adb/shell/ws", nil)
	rejected.Header.Set("Origin", "http://evil.com")
	if checkOrigin(rejected) {
		t.Fatal("cross-site upgrade was accepted")
	}
}

func TestStateChangingAllowed(t *testing.T) {
	server := NewServer(Config{LoopbackPort: 7575})
	newRequest := func(method, contentType, origin, site string) *http.Request {
		request := httptest.NewRequest(method, "http://127.0.0.1:7575/api/v1/device/actions/reboot", strings.NewReader(`{}`))
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if site != "" {
			request.Header.Set("Sec-Fetch-Site", site)
		}
		return request
	}
	for _, check := range []struct {
		name string
		req  *http.Request
		want bool
	}{
		{name: "json same origin", req: newRequest(http.MethodPost, "application/json", "http://127.0.0.1:7575", "same-origin"), want: true},
		{name: "json charset", req: newRequest(http.MethodPost, "application/json; charset=utf-8", "http://localhost:7575", ""), want: true},
		{name: "ipv6 origin", req: newRequest(http.MethodPost, "application/json", "http://[::1]:7575", "same-site"), want: true},
		{name: "no content type same origin", req: newRequest(http.MethodPost, "", "http://127.0.0.1:7575", ""), want: true},
		{name: "cross-site text/plain", req: newRequest(http.MethodPost, "text/plain", "http://127.0.0.1:7575", "same-origin"), want: false},
		{name: "form content type", req: newRequest(http.MethodPost, "application/x-www-form-urlencoded", "http://127.0.0.1:7575", "same-origin"), want: false},
		{name: "missing origin rejected", req: newRequest(http.MethodPost, "application/json", "", ""), want: false},
		{name: "evil origin", req: newRequest(http.MethodPost, "application/json", "http://evil.com", "cross-site"), want: false},
		{name: "cross-site sec-fetch-site", req: newRequest(http.MethodPost, "application/json", "http://127.0.0.1:7575", "cross-site"), want: false},
		{name: "origin wrong port", req: newRequest(http.MethodPost, "application/json", "http://127.0.0.1:9999", ""), want: false},
		{name: "get not state changing", req: newRequest(http.MethodGet, "", "", ""), want: !isStateChangingMethod(http.MethodGet)},
		{name: "dev proxy origin matches host port", req: func() *http.Request {
			request := newRequest(http.MethodPost, "application/json", "http://127.0.0.1:5176", "same-origin")
			request.Host = "127.0.0.1:5176"
			return request
		}(), want: true},
		{name: "dev proxy origin port mismatch", req: func() *http.Request {
			request := newRequest(http.MethodPost, "application/json", "http://127.0.0.1:8080", "same-origin")
			request.Host = "127.0.0.1:5176"
			return request
		}(), want: false},
	} {
		t.Run(check.name, func(t *testing.T) {
			allowed := !isStateChangingMethod(check.req.Method) || server.stateChangingAllowed(check.req)
			if allowed != check.want {
				t.Errorf("state-changing guard = %v, want %v", allowed, check.want)
			}
		})
	}
}

// TestLoopbackBoundaryRejectsCrossSiteWrites drives real HTTP requests at a
// live listener: cross-site text/plain and empty-body writes are rejected
// before any service runs, and same-origin writes over the three accepted
// loopback forms are accepted.
func TestLoopbackBoundaryRejectsCrossSiteWrites(t *testing.T) {
	server := newTestServer(t, nil)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	server.SetLoopbackPort(port)

	post := func(path, contentType, origin, site, body string) *http.Response {
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		request, err := http.NewRequest(http.MethodPost, ts.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if site != "" {
			request.Header.Set("Sec-Fetch-Site", site)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response
	}

	// Cross-site text/plain is a simple request a malicious page can send.
	if response := post("/api/v1/notifications/debug", "text/plain", "http://evil.com", "cross-site", `{"action":"call_incoming"}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-site text/plain status = %d, want 400", response.StatusCode)
	}
	// Cross-site empty-body POST (no Content-Type, no body) is rejected by origin.
	if response := post("/api/v1/notifications/debug", "", "http://evil.com", "cross-site", ""); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-site empty-body status = %d, want 400", response.StatusCode)
	}
	// A same-origin write with text/plain is still rejected.
	if response := post("/api/v1/notifications/debug", "text/plain", fmt.Sprintf("http://127.0.0.1:%d", port), "same-origin", `{"action":"call_incoming"}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("same-origin text/plain status = %d, want 400", response.StatusCode)
	}

	for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
		origin := fmt.Sprintf("http://%s:%d", host, port)
		response := post("/api/v1/notifications/debug", "application/json", origin, "same-origin", `{"action":"call_incoming","call_id":"x"}`)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("same-origin %s status = %d, want 200", host, response.StatusCode)
		}
	}

	// A same-origin dev proxy (e.g. Vite serving the UI on another port and
	// proxying /api to this server) preserves the browser-facing Host header:
	// the Origin port then matches the Host port rather than the bound port.
	proxyRequest := func(hostPort, origin string) *http.Response {
		request, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/notifications/debug", strings.NewReader(`{"action":"call_incoming","call_id":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Host = hostPort
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response
	}
	proxyPort := port + 1
	if response := proxyRequest(fmt.Sprintf("127.0.0.1:%d", proxyPort), fmt.Sprintf("http://localhost:%d", proxyPort)); response.StatusCode != http.StatusOK {
		t.Fatalf("dev-proxy same-origin write status = %d, want 200", response.StatusCode)
	}
	if response := proxyRequest(fmt.Sprintf("127.0.0.1:%d", proxyPort), fmt.Sprintf("http://127.0.0.1:%d", proxyPort+1)); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("dev-proxy mismatched origin port status = %d, want 400", response.StatusCode)
	}
}

// TestLoopbackBoundaryRejectsBadWebSocketOrigin exercises the event WebSocket
// upgrade: disallowed origins and non-loopback Host headers are rejected, and
// allowed same-origin upgrades succeed.
func TestLoopbackBoundaryRejectsBadWebSocketOrigin(t *testing.T) {
	server := newTestServer(t, nil)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	server.SetLoopbackPort(port)

	dial := func(url, origin string, dialer *websocket.Dialer) error {
		requestHeader := http.Header{}
		if origin != "" {
			requestHeader.Set("Origin", origin)
		}
		conn, _, err := dialer.Dial(url, requestHeader)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}

	redirectToServer := func(network, _ string) (net.Conn, error) {
		return net.Dial(network, fmt.Sprintf("127.0.0.1:%d", port))
	}
	for _, check := range []struct {
		name   string
		url    string
		origin string
		dialer *websocket.Dialer
	}{
		{name: "evil origin", url: fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events/ws", port), origin: "http://evil.com", dialer: websocket.DefaultDialer},
		{name: "other local origin", url: fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events/ws", port), origin: fmt.Sprintf("http://127.0.0.1:%d", port+1), dialer: websocket.DefaultDialer},
		{name: "host header not loopback", url: fmt.Sprintf("ws://evil.com:%d/api/v1/events/ws", port), origin: "http://evil.com", dialer: &websocket.Dialer{NetDial: redirectToServer}},
		{name: "proxy origin port mismatch", url: fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events/ws", port+1), origin: fmt.Sprintf("http://127.0.0.1:%d", port+2), dialer: &websocket.Dialer{NetDial: redirectToServer}},
	} {
		t.Run(check.name, func(t *testing.T) {
			if err := dial(check.url, check.origin, check.dialer); err == nil {
				t.Fatal("upgrade succeeded, want rejection")
			}
		})
	}

	for _, host := range []string{"127.0.0.1", "localhost", "127.0.0.1"} {
		url := fmt.Sprintf("ws://%s:%d/api/v1/events/ws", host, port)
		origin := fmt.Sprintf("http://%s:%d", host, port)
		t.Run("allowed "+host, func(t *testing.T) {
			if err := dial(url, origin, websocket.DefaultDialer); err != nil {
				t.Fatalf("same-origin upgrade failed: %v", err)
			}
		})
	}

	// A same-origin dev proxy preserves the browser-facing Host header, so the
	// Origin port matches the Host port rather than the bound port.
	t.Run("allowed dev proxy origin", func(t *testing.T) {
		url := fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events/ws", port+1)
		origin := fmt.Sprintf("http://localhost:%d", port+1)
		if err := dial(url, origin, &websocket.Dialer{NetDial: redirectToServer}); err != nil {
			t.Fatalf("dev-proxy same-origin upgrade failed: %v", err)
		}
	})
}

// TestOpenAPIDoesNotDeclareDeferredAuthentication guards the temporary
// boundary: the OpenAPI document must not advertise login credentials or a
// security scheme the server does not enforce.
func TestOpenAPIDoesNotDeclareDeferredAuthentication(t *testing.T) {
	server := newTestServer(t, nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("openapi status = %d", recorder.Code)
	}
	document := openAPIDocument()
	root := document["openapi"]
	if root == nil {
		t.Fatal("openapi document is missing")
	}
	if _, present := document["security"]; present {
		t.Fatal("openapi document declares top-level security")
	}
	components, ok := document["components"].(map[string]any)
	if !ok {
		t.Fatal("openapi components missing")
	}
	if _, present := components["securitySchemes"]; present {
		t.Fatal("openapi document declares securitySchemes")
	}
	for _, path := range document["paths"].(map[string]any) {
		for _, operation := range path.(map[string]any) {
			if _, present := operation.(map[string]any)["security"]; present {
				t.Fatalf("openapi operation declares security: %#v", path)
			}
		}
	}
}
