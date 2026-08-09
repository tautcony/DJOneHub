package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/domain/device"
)

type fakeDiscovery struct {
	mu         sync.Mutex
	candidates []device.Candidate
}

func (f *fakeDiscovery) Discover(context.Context) ([]device.Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]device.Candidate(nil), f.candidates...), nil
}

type fakeBackend struct {
	mode   string
	caps   device.CapabilitySet
	closed bool
	events chan backend.BackendEvent
}

func (f *fakeBackend) Mode() string { return f.mode }
func (f *fakeBackend) Identity(context.Context) (backend.Identity, error) {
	return backend.Identity{IMEI: "123"}, nil
}
func (f *fakeBackend) Radio(context.Context) (backend.RadioState, error) {
	return backend.RadioState{}, nil
}
func (f *fakeBackend) SIM(context.Context) (backend.SIMState, error)         { return backend.SIMState{}, nil }
func (f *fakeBackend) ListSMS(context.Context) ([]backend.SMSMessage, error) { return nil, nil }
func (f *fakeBackend) SendSMS(context.Context, string, string) error         { return nil }
func (f *fakeBackend) USSD(context.Context, string) (string, error)          { return "", nil }
func (f *fakeBackend) APDU(context.Context, backend.APDURequest) (backend.APDUResponse, error) {
	return backend.APDUResponse{}, nil
}
func (f *fakeBackend) Capabilities(context.Context) device.CapabilitySet { return f.caps }
func (f *fakeBackend) Events(context.Context) (<-chan backend.BackendEvent, error) {
	if f.events != nil {
		return f.events, nil
	}
	return make(chan backend.BackendEvent), nil
}
func (f *fakeBackend) SetInboundSMSHandler(backend.InboundSMSHandler) {}
func (f *fakeBackend) Close() error                                   { f.closed = true; return nil }

type fakeFactory struct {
	b   backend.ModemBackend
	err error
}

func (f *fakeFactory) Open(context.Context, device.Candidate) (backend.ModemBackend, string, error) {
	return f.b, "fake", f.err
}

func TestRuntimeReachesReadyWithoutHardware(t *testing.T) {
	d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}}}}
	r, err := New(Config{Discovery: d, Backends: &fakeFactory{b: &fakeBackend{mode: "mbim", caps: device.CapabilitySet{device.CapabilityDeviceStatus: ""}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.State != device.StateReady || s.Backend != device.BackendMBIM || !s.Capabilities.Has(device.CapabilityDeviceStatus) {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
}

func TestRuntimeFakeHardwareCoversAllBackendModes(t *testing.T) {
	for _, mode := range []string{"at", "qmi", "mbim"} {
		t.Run(mode, func(t *testing.T) {
			d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "fake-" + mode}}}}
			r, err := New(Config{Discovery: d, Backends: &fakeFactory{b: &fakeBackend{mode: mode}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Rescan(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := r.Snapshot().State; got != device.StateReady {
				t.Fatalf("mode=%s state=%s", mode, got)
			}
		})
	}
}

func TestRuntimeDisconnectsWhenDeviceDisappears(t *testing.T) {
	d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}}}}
	b := &fakeBackend{mode: "at"}
	r, _ := New(Config{Discovery: d, Backends: &fakeFactory{b: b}})
	_ = r.Rescan(context.Background())
	d.mu.Lock()
	d.candidates = nil
	d.mu.Unlock()
	_ = r.Rescan(context.Background())
	if got := r.Snapshot().State; got != device.StateAbsent {
		t.Fatalf("state = %s", got)
	}
	if !b.closed {
		t.Fatal("backend was not closed")
	}
	traces := r.Events().RecentMessageTraces()
	if len(traces) < 4 {
		t.Fatalf("disconnect traces = %#v", traces)
	}
	changes := traces[len(traces)-4:]
	want := [][3]string{
		{"device.status.changed", "ready", "disconnected"},
		{"device.offline", "ready", "disconnected"},
		{"device.status.changed", "disconnected", "absent"},
		{"device.offline", "disconnected", "absent"},
	}
	for i, expected := range want {
		if changes[i].Type != expected[0] || changes[i].Fields["previous_state"] != expected[1] || changes[i].Fields["state"] != expected[2] {
			t.Fatalf("disconnect trace %d = %#v, want %v", i, changes[i], expected)
		}
	}
}

func TestResourceLockCanBeCancelled(t *testing.T) {
	locks := NewResourceLocks()
	release, err := locks.Acquire(context.Background(), ResourceSIM)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := locks.Acquire(ctx, ResourceSIM); err == nil || !errors.Is(err, context.DeadlineExceeded) && err.Error() == "" {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestRuntimeCorrelatesReenumeratedPorts(t *testing.T) {
	d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}, ATPort: "/dev/tty.1"}}}
	r, _ := New(Config{Discovery: d, Backends: &fakeFactory{b: &fakeBackend{mode: "mbim"}}})
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.candidates = []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}, ATPort: "/dev/tty.2"}}
	d.mu.Unlock()
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	candidate, err := r.Candidate()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ATPort != "/dev/tty.2" || r.Snapshot().State != device.StateReady {
		t.Fatalf("candidate = %+v, snapshot = %+v", candidate, r.Snapshot())
	}
}

func TestRuntimeRetriesDegradedInitialization(t *testing.T) {
	d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}}}}
	factory := &fakeFactory{err: errors.New("temporary backend error")}
	r, _ := New(Config{Discovery: d, Backends: factory, PollInterval: time.Hour, ReconnectDelay: time.Millisecond})
	if err := r.Rescan(context.Background()); err == nil {
		t.Fatal("expected initialization error")
	}
	if r.Snapshot().State != device.StateDegraded {
		t.Fatalf("state = %s", r.Snapshot().State)
	}
	factory.err = nil
	factory.b = &fakeBackend{mode: "qmi"}
	time.Sleep(2 * time.Millisecond)
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.Snapshot().State != device.StateReady {
		t.Fatalf("state = %s", r.Snapshot().State)
	}
}

func TestRuntimeFansBackendEventsOutToFeatureTopics(t *testing.T) {
	d := &fakeDiscovery{candidates: []device.Candidate{{Identity: device.Identity{StableID: "slot-1"}}}}
	events := make(chan backend.BackendEvent, 2)
	b := &fakeBackend{mode: "mbim", events: events}
	r, err := New(Config{Discovery: d, Backends: &fakeFactory{b: b}})
	if err != nil {
		t.Fatal(err)
	}
	_, received, unsubscribe := r.Events().Subscribe(32)
	defer unsubscribe()
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	events <- backend.BackendEvent{Type: "sms.received", Data: map[string]any{"index": 4}}

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-received:
			if event.Type == "sms.updated" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for sms.updated")
		}
	}
}
