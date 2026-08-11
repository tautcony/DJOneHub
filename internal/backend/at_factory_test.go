package backend

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/modem"
)

type factoryATTransport struct {
	mu          sync.Mutex
	responses   chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	readTimeout time.Duration
}

func newFactoryATTransport() *factoryATTransport {
	return &factoryATTransport{
		responses: make(chan []byte, 64),
		closed:    make(chan struct{}),
	}
}

func (t *factoryATTransport) Read(buffer []byte) (int, error) {
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

func (t *factoryATTransport) Write(payload []byte) (int, error) {
	select {
	case <-t.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	command := strings.TrimSpace(string(payload))
	response := "\r\nOK\r\n"
	switch {
	case strings.HasPrefix(command, "AT+CCHO="):
		response = "\r\n+CCHO: 2\r\nOK\r\n"
	case strings.HasPrefix(command, "AT+CGLA="):
		response = "\r\n+CGLA: 4,\"9000\"\r\nOK\r\n"
	}
	t.responses <- []byte(response)
	return len(payload), nil
}

func (t *factoryATTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

func (t *factoryATTransport) SetReadTimeout(timeout time.Duration) error {
	t.mu.Lock()
	t.readTimeout = timeout
	t.mu.Unlock()
	return nil
}

func TestATFactoryUsesInjectedTransportWithSharedBackend(t *testing.T) {
	transport := newFactoryATTransport()
	opened := false
	factory := NewATFactory(func(context.Context, device.Candidate) (modem.ATTransport, error) {
		opened = true
		return transport, nil
	})
	esimBuilt := false
	factory.ESIMPort = func(manager *modem.Manager, arbiter *apduarbiter.Arbiter, _ device.Candidate) (ESIMPort, error) {
		if manager == nil || arbiter == nil {
			t.Fatal("eSIM builder did not receive the shared manager and arbiter")
		}
		esimBuilt = true
		return &stubESIMPort{}, nil
	}

	business, source, err := factory.Open(context.Background(), device.Candidate{Identity: device.Identity{
		StableID: "injected-at-factory-test",
		Product:  "synthetic AT modem",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()
	if !opened || !esimBuilt {
		t.Fatalf("opened=%v esimBuilt=%v", opened, esimBuilt)
	}
	if source != "selected platform AT transport" {
		t.Fatalf("source = %q", source)
	}

	adapter, ok := business.(*BusinessAdapter)
	if !ok {
		t.Fatalf("backend type = %T, want *BusinessAdapter", business)
	}
	at, ok := adapter.Legacy().(*ATBackend)
	if !ok || at.Modem() == nil {
		t.Fatalf("legacy backend = %T, want shared ATBackend", adapter.Legacy())
	}
	if raw, err := business.(RawATBackend).RawAT(context.Background(), "AT+CSQ"); err != nil || raw != "OK" {
		t.Fatalf("RawAT() = %q, %v", raw, err)
	}

	channel, err := at.OpenLogicalChannel(context.Background(), "A0000000871002")
	if err != nil || channel != 2 {
		t.Fatalf("OpenLogicalChannel() = %d, %v", channel, err)
	}
	response, err := at.TransmitAPDU(context.Background(), channel, "00A40000")
	if err != nil || response != "9000" {
		t.Fatalf("TransmitAPDU() = %q, %v", response, err)
	}
	if err := at.CloseLogicalChannel(context.Background(), channel); err != nil {
		t.Fatal(err)
	}

	if err := business.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.closed:
	default:
		t.Fatal("backend did not close the injected transport")
	}
}
