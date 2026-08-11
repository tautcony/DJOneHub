package device

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

type statusTestDiscovery struct {
	mu        sync.Mutex
	candidate domain.Candidate
}

func (d *statusTestDiscovery) Discover(context.Context) ([]domain.Candidate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return []domain.Candidate{d.candidate}, nil
}

type statusTestFactory struct{ backend *statusTestBackend }

func (f *statusTestFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return f.backend, "test", nil
}

type statusTestBackend struct {
	identityCalls atomic.Int32
	radioCalls    atomic.Int32
	simCalls      atomic.Int32
	identityStart chan struct{}
	identityGate  chan struct{}
	radioErr      error
}

func (b *statusTestBackend) Mode() string { return backend.BackendAT }
func (b *statusTestBackend) Identity(context.Context) (backend.Identity, error) {
	b.identityCalls.Add(1)
	if b.identityStart != nil {
		select {
		case b.identityStart <- struct{}{}:
		default:
		}
	}
	if b.identityGate != nil {
		<-b.identityGate
	}
	return backend.Identity{IMEI: "test-imei", IMSI: "test-imsi", ICCID: "test-iccid"}, nil
}
func (b *statusTestBackend) Radio(context.Context) (backend.RadioState, error) {
	b.radioCalls.Add(1)
	if b.radioErr != nil {
		return backend.RadioState{}, b.radioErr
	}
	return backend.RadioState{Registered: true, Operator: "test-network"}, nil
}
func (b *statusTestBackend) SIM(context.Context) (backend.SIMState, error) {
	b.simCalls.Add(1)
	return backend.SIMState{Inserted: true, IMSI: "test-imsi", ICCID: "test-iccid"}, nil
}
func (b *statusTestBackend) ListSMS(context.Context) ([]backend.SMSMessage, error) { return nil, nil }
func (b *statusTestBackend) SendSMS(context.Context, string, string) error         { return nil }
func (b *statusTestBackend) USSD(context.Context, string) (string, error)          { return "", nil }
func (b *statusTestBackend) APDU(context.Context, backend.APDURequest) (backend.APDUResponse, error) {
	return backend.APDUResponse{}, nil
}
func (b *statusTestBackend) Capabilities(context.Context) domain.CapabilitySet {
	return domain.CapabilitySet{domain.CapabilityDeviceStatus: "test"}
}
func (b *statusTestBackend) Events(context.Context) (<-chan backend.BackendEvent, error) {
	ch := make(chan backend.BackendEvent)
	return ch, nil
}
func (b *statusTestBackend) SetInboundSMSHandler(backend.InboundSMSHandler) {}
func (b *statusTestBackend) Close() error                                   { return nil }

func newReadyStatusService(t *testing.T, discovery *statusTestDiscovery, b *statusTestBackend) (*Service, *runtime.Runtime, *statusTestFactory) {
	t.Helper()
	factory := &statusTestFactory{backend: b}
	rt, err := runtime.New(runtime.Config{Discovery: discovery, Backends: factory})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := NewService(rt)
	service.ttl = time.Second
	return service, rt, factory
}

func TestStatusCoalescesGenerationScopedReadsAndReusesTTL(t *testing.T) {
	discovery := &statusTestDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "slot-a"}}}
	b := &statusTestBackend{}
	service, rt, factory := newReadyStatusService(t, discovery, b)
	b.identityCalls.Store(0)
	b.identityStart = make(chan struct{}, 2)
	b.identityGate = make(chan struct{})

	results := make(chan Status, 2)
	errs := make(chan error, 2)
	start := func() {
		go func() {
			status, err := service.Status(context.Background())
			results <- status
			errs <- err
		}()
	}
	start()
	select {
	case <-b.identityStart:
	case <-time.After(time.Second):
		t.Fatal("identity read did not start")
	}
	start()
	close(b.identityGate)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		<-results
	}
	if got := b.identityCalls.Load(); got != 1 {
		t.Fatalf("identity calls=%d want 1", got)
	}
	if got := b.radioCalls.Load(); got != 1 {
		t.Fatalf("radio calls=%d want 1", got)
	}
	if got := b.simCalls.Load(); got != 1 {
		t.Fatalf("sim calls=%d want 1", got)
	}
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.identityCalls.Load() != 1 || b.radioCalls.Load() != 1 || b.simCalls.Load() != 1 {
		t.Fatalf("TTL cache missed: identity=%d radio=%d sim=%d", b.identityCalls.Load(), b.radioCalls.Load(), b.simCalls.Load())
	}

	// A new runtime generation must bypass every previous snapshot.
	newBackend := &statusTestBackend{}
	discovery.mu.Lock()
	discovery.candidate = domain.Candidate{Identity: domain.Identity{StableID: "slot-b"}}
	discovery.mu.Unlock()
	factory.backend = newBackend
	if err := rt.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if newBackend.identityCalls.Load() != 2 || newBackend.radioCalls.Load() != 1 || newBackend.simCalls.Load() != 1 {
		t.Fatalf("generation cache was reused: identity=%d radio=%d sim=%d", newBackend.identityCalls.Load(), newBackend.radioCalls.Load(), newBackend.simCalls.Load())
	}
}

func TestStatusDoesNotReplaceCachedRadioOnFailedRefresh(t *testing.T) {
	discovery := &statusTestDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "slot-a"}}}
	b := &statusTestBackend{}
	service, _, _ := newReadyStatusService(t, discovery, b)
	b.identityCalls.Store(0)
	first, err := service.Status(context.Background())
	if err != nil || first.Radio.Operator != "test-network" {
		t.Fatalf("first status=%+v err=%v", first, err)
	}
	b.radioErr = errors.New("radio unavailable")
	service.ttl = time.Millisecond
	time.Sleep(3 * time.Millisecond)
	second, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Radio.Operator != "" || second.Snapshot.LastError != "radio unavailable" {
		t.Fatalf("failed refresh status=%+v", second)
	}
	if b.radioCalls.Load() < 2 {
		t.Fatalf("radio calls=%d want refresh", b.radioCalls.Load())
	}
}
