package modem

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

type scriptedATTransport struct {
	mu          sync.Mutex
	responses   chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	readTimeout time.Duration
	writes      []string
}

func newScriptedATTransport() *scriptedATTransport {
	return &scriptedATTransport{
		responses: make(chan []byte, 64),
		closed:    make(chan struct{}),
	}
}

func (t *scriptedATTransport) Read(buffer []byte) (int, error) {
	t.mu.Lock()
	timeout := t.readTimeout
	t.mu.Unlock()
	if timeout <= 0 {
		timeout = time.Second
	}
	select {
	case response := <-t.responses:
		return copy(buffer, response), nil
	case <-t.closed:
		return 0, io.EOF
	case <-time.After(timeout):
		return 0, errors.New("AT transport timeout")
	}
}

func (t *scriptedATTransport) Write(payload []byte) (int, error) {
	select {
	case <-t.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	t.mu.Lock()
	t.writes = append(t.writes, string(payload))
	t.mu.Unlock()
	if strings.HasSuffix(string(payload), "\r\n") {
		response := "\r\nOK\r\n"
		if strings.HasPrefix(strings.TrimSpace(string(payload)), "AT+FAIL") {
			response = "\r\nERROR\r\n"
		}
		t.responses <- []byte(response)
	}
	return len(payload), nil
}

func TestManagerInjectedTransportClassifiesTerminalErrors(t *testing.T) {
	transport := newScriptedATTransport()
	manager, err := NewWithATTransport(config.DeviceConfig{ID: "injected-at-error-test", DeviceBackend: "at"}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	if !manager.WaitReady(3 * time.Second) {
		t.Fatal("injected transport manager did not become ready")
	}
	if _, err := manager.ExecuteAT("AT+FAIL", time.Second); err == nil {
		t.Fatal("ExecuteAT() error = nil for terminal ERROR")
	}
	if !manager.StopAndWait(time.Second) {
		t.Fatal("manager did not stop after terminal error test")
	}
}

func (t *scriptedATTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

func (t *scriptedATTransport) SetReadTimeout(timeout time.Duration) error {
	t.mu.Lock()
	t.readTimeout = timeout
	t.mu.Unlock()
	return nil
}

func TestManagerUsesInjectedATTransport(t *testing.T) {
	transport := newScriptedATTransport()
	manager, err := NewWithATTransport(config.DeviceConfig{
		ID:            "injected-at-test",
		DeviceBackend: "at",
	}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if !manager.HasATPort() {
		t.Fatal("HasATPort() = false for injected transport")
	}
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	if !manager.WaitReady(3 * time.Second) {
		t.Fatal("injected transport manager did not become ready")
	}
	response, err := manager.ExecuteATRaw("AT+CSQ", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response != "OK" {
		t.Fatalf("ExecuteATRaw() = %q, want OK", response)
	}

	transport.mu.Lock()
	readTimeout := transport.readTimeout
	writes := append([]string(nil), transport.writes...)
	transport.mu.Unlock()
	if readTimeout != 100*time.Millisecond {
		t.Fatalf("read timeout = %s, want 100ms", readTimeout)
	}
	if !containsATWrite(writes, "AT+CSQ\r\n") {
		t.Fatalf("writes do not contain AT+CSQ: %q", writes)
	}

	if !manager.StopAndWait(time.Second) {
		t.Fatal("manager did not stop after closing injected transport")
	}
	select {
	case <-transport.closed:
	default:
		t.Fatal("manager did not close the injected transport")
	}
}

func TestNewWithATTransportRejectsNil(t *testing.T) {
	if _, err := NewWithATTransport(config.DeviceConfig{ID: "nil-at-test"}, nil); err == nil {
		t.Fatal("NewWithATTransport() error = nil")
	}
}

func containsATWrite(writes []string, target string) bool {
	for _, write := range writes {
		if write == target {
			return true
		}
	}
	return false
}
