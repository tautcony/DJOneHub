package network

import (
	"context"
	"errors"
	"testing"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

type fakeDiscovery struct{ candidate domain.Candidate }

func (f fakeDiscovery) Discover(context.Context) ([]domain.Candidate, error) {
	return []domain.Candidate{f.candidate}, nil
}

type fakeNetworkBackend struct{ unavailable bool }

func (f *fakeNetworkBackend) Mode() string { return "qmi" }
func (f *fakeNetworkBackend) Identity(context.Context) (backend.Identity, error) {
	return backend.Identity{IMEI: "123456789012345"}, nil
}
func (f *fakeNetworkBackend) Radio(context.Context) (backend.RadioState, error) {
	return backend.RadioState{}, nil
}
func (f *fakeNetworkBackend) SIM(context.Context) (backend.SIMState, error) {
	return backend.SIMState{}, nil
}
func (f *fakeNetworkBackend) ListSMS(context.Context) ([]backend.SMSMessage, error) {
	return nil, nil
}
func (f *fakeNetworkBackend) SendSMS(context.Context, string, string) error { return nil }
func (f *fakeNetworkBackend) USSD(context.Context, string) (string, error)  { return "", nil }
func (f *fakeNetworkBackend) APDU(context.Context, backend.APDURequest) (backend.APDUResponse, error) {
	return backend.APDUResponse{}, nil
}
func (f *fakeNetworkBackend) Capabilities(context.Context) domain.CapabilitySet {
	return domain.CapabilitySet{
		domain.CapabilityDeviceStatus:   "fake",
		domain.CapabilityNetworkStatus:  "fake",
		domain.CapabilityNetworkControl: "fake",
	}
}
func (f *fakeNetworkBackend) Events(context.Context) (<-chan backend.BackendEvent, error) {
	ch := make(chan backend.BackendEvent)
	close(ch)
	return ch, nil
}
func (f *fakeNetworkBackend) Close() error { return nil }
func (f *fakeNetworkBackend) Status(context.Context) (map[string]any, error) {
	if f.unavailable {
		return nil, errors.New("network unavailable")
	}
	return map[string]any{"interface": "wwan0", "rx_bytes": uint64(1), "tx_bytes": uint64(2)}, nil
}
func (f *fakeNetworkBackend) SetMode(context.Context, string) error {
	if f.unavailable {
		return errors.New("network unavailable")
	}
	return nil
}
func (f *fakeNetworkBackend) Traffic(context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *fakeNetworkBackend) Check(context.Context) (map[string]any, error) {
	if f.unavailable {
		return nil, errors.New("network unavailable")
	}
	return map[string]any{"ok": true, "summary": "reachable"}, nil
}

type fakeFactory struct{ backend *fakeNetworkBackend }

func (f fakeFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return f.backend, "fake QMI", nil
}

func TestNetworkServiceUsesFakeBackendAndReportsInjectedFailure(t *testing.T) {
	fake := &fakeNetworkBackend{unavailable: true}
	r, err := runtime.New(runtime.Config{
		Discovery: fakeDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "fake-network"}}},
		Backends:  fakeFactory{backend: fake},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	devices := device.NewService(r)
	service := NewService(devices, operation.NewManager(r.Events()), r, nil)
	if _, err := service.Check(context.Background()); err == nil || err.Error() != "network unavailable" {
		t.Fatalf("Check error=%v, want injected network failure", err)
	}

	fake.unavailable = false
	status, err := service.Status(context.Background())
	if err != nil || status.Interface != "wwan0" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
