package httpapi

import (
	"mime"
	"net"
	"net/url"
	nethttp "net/http"
	"strconv"
	"strings"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// IsLoopbackHost reports whether host (with IPv6 brackets already stripped)
// is one of the loopback forms the temporary boundary accepts: 127.0.0.1,
// localhost, or ::1.
func IsLoopbackHost(host string) bool {
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// loopbackHostPort reports whether hostport names a loopback form on the
// bound port, e.g. "127.0.0.1:7575", "localhost:7575", or "[::1]:7575".
func loopbackHostPort(hostport string, port int) bool {
	if port <= 0 {
		return false
	}
	host, portPart, err := net.SplitHostPort(hostport)
	if err != nil || portPart != strconv.Itoa(port) {
		return false
	}
	return IsLoopbackHost(host)
}

// splitLoopbackHostPort splits hostport and reports whether it names a
// loopback form (127.0.0.1, localhost, ::1), returning the port part on
// success. Browsers and proxies address loopback with any port, so the port
// is validated against the bound port or the Host port by the callers.
func splitLoopbackHostPort(hostport string) (string, bool) {
	host, portPart, err := net.SplitHostPort(hostport)
	if err != nil || !IsLoopbackHost(host) {
		return "", false
	}
	return portPart, true
}

// loopbackOriginAllowed validates the Host header and, when present, the
// Origin header of a WebSocket upgrade: both must name a loopback form
// (127.0.0.1, localhost, ::1). The Origin port must be the bound port (direct
// access, or a host-rewriting proxy) or the Host-header port, which a
// same-origin dev proxy (e.g. Vite proxying to the API) preserves — a page
// served by a local dev server on another port legitimately carries that
// server's port, while a page on any other local port stays blocked. A missing
// Origin (non-browser client) is accepted only when the client addressed the
// bound loopback port directly; the loopback Host check still blocks
// DNS-rebinding upgrades.
func (s *Server) loopbackOriginAllowed(r *nethttp.Request) bool {
	hostPort, ok := splitLoopbackHostPort(r.Host)
	if !ok {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return hostPort == strconv.Itoa(s.config.LoopbackPort)
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	originPort, ok := splitLoopbackHostPort(parsed.Host)
	if !ok {
		return false
	}
	return originPort == strconv.Itoa(s.config.LoopbackPort) || originPort == hostPort
}

func isStateChangingMethod(method string) bool {
	switch method {
	case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions:
		return false
	default:
		return true
	}
}

// stateChangingAllowed applies the temporary loopback boundary to a
// state-changing request: body-bearing writes must use application/json, a
// supplied Sec-Fetch-Site must not be cross-site, and the Origin must be a
// loopback form on the bound port. Missing Origin or Sec-Fetch-Site metadata
// is rejected for state-changing requests; a browser always supplies both.
func (s *Server) stateChangingAllowed(r *nethttp.Request) bool {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return false
		}
	}
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "", "same-origin", "same-site", "none":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	originPort, ok := splitLoopbackHostPort(parsed.Host)
	if !ok {
		return false
	}
	// Same rule as WebSocket upgrades: the page's origin port must be the
	// bound port or the port the browser connected to (Host header), so a
	// same-origin dev proxy is allowed while any other local port is not.
	hostPort, _ := splitLoopbackHostPort(r.Host)
	return originPort == strconv.Itoa(s.config.LoopbackPort) || (hostPort != "" && originPort == hostPort)
}

// loopbackGuard rejects state-changing requests that fail the temporary
// loopback boundary before any application service runs. WebSocket upgrades
// are GET requests and carry their own Origin/Host validation in the upgrade
// handlers.
func (s *Server) loopbackGuard(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !isStateChangingMethod(r.Method) || s.stateChangingAllowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, derrors.New(derrors.InvalidRequest, "cross-site request rejected", false, nil))
	})
}
