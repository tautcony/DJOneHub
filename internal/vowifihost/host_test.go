package vowifihost

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
)

type fakePort struct{}

func (fakePort) Enable(context.Context) error    { return nil }
func (fakePort) Disable(context.Context) error   { return nil }
func (fakePort) Reconnect(context.Context) error { return nil }
func (fakePort) Status(context.Context) (map[string]any, error) {
	return map[string]any{"state": "connected"}, nil
}

type fakeFactory struct{}

func (fakeFactory) Open(context.Context) (backend.VoWiFiPort, error) { return fakePort{}, nil }

func TestHostLifecycle(t *testing.T) {
	h := New(fakeFactory{}, nil)
	if err := h.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.State() != Connected {
		t.Fatalf("state = %s", h.State())
	}
	h.DeviceRemoved()
	if h.State() != Recovering {
		t.Fatalf("state = %s", h.State())
	}
	if err := h.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.State() != Connected {
		t.Fatalf("state = %s", h.State())
	}
	if err := h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.State() != Disabled {
		t.Fatalf("state = %s", h.State())
	}
}
