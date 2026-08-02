package vowifihost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/runtime"
)

type dependencyFactory struct {
	checkErr error
	opens    int
	port     *eventPort
}

func (f *dependencyFactory) CheckVoWiFiDependencies(context.Context) error { return f.checkErr }
func (f *dependencyFactory) Open(context.Context) (backend.VoWiFiPort, error) {
	f.opens++
	return f.port, nil
}

type eventPort struct{ events chan Event }

type recoveringPort struct {
	events     chan Event
	reconnects int
}

func (p *eventPort) Enable(context.Context) error    { return nil }
func (p *eventPort) Disable(context.Context) error   { return nil }
func (p *eventPort) Reconnect(context.Context) error { return nil }
func (p *eventPort) Status(context.Context) (map[string]any, error) {
	return map[string]any{"state": "connected"}, nil
}
func (p *eventPort) Events(context.Context) (<-chan Event, error) { return p.events, nil }

func (p *recoveringPort) Enable(context.Context) error    { return nil }
func (p *recoveringPort) Disable(context.Context) error   { return nil }
func (p *recoveringPort) Reconnect(context.Context) error { p.reconnects++; return nil }
func (p *recoveringPort) Status(context.Context) (map[string]any, error) {
	return map[string]any{"state": "connected"}, nil
}
func (p *recoveringPort) Events(context.Context) (<-chan Event, error) { return p.events, nil }

func TestHostValidatesDependenciesBeforeOpeningSession(t *testing.T) {
	factory := &dependencyFactory{checkErr: errors.New("packet tunnel unavailable"), port: &eventPort{}}
	host := New(factory, nil)
	if err := host.Enable(context.Background()); err == nil || err.Error() != "packet tunnel unavailable" {
		t.Fatalf("Enable error=%v", err)
	}
	if factory.opens != 0 || host.State() != Failed {
		t.Fatalf("opens=%d state=%s", factory.opens, host.State())
	}
}

func TestHostMapsTunnelAndIMSStateEventsAndRecoversAfterDeviceReady(t *testing.T) {
	port := &eventPort{events: make(chan Event, 2)}
	factory := &dependencyFactory{port: port}
	host := New(factory, nil)
	_, events, unsubscribe := host.Events().Subscribe(16)
	defer unsubscribe()
	if err := host.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	port.events <- Event{Type: "tunnel.changed", Data: map[string]any{"state": "up"}}
	port.events <- Event{Type: "ims.changed", Data: map[string]any{"state": "registered"}}
	waitForHostEvent(t, events, "vowifi.tunnel.changed")
	waitForHostEvent(t, events, "vowifi.ims.changed")

	host.DeviceRemoved()
	if host.State() != Recovering {
		t.Fatalf("state after removal=%s", host.State())
	}
	host.DeviceReady()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if host.State() == Connected && factory.opens >= 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("host did not recover: state=%s opens=%d", host.State(), factory.opens)
}

func TestHostRecoversAfterResetAndDoesNotRecoverWhenDisabled(t *testing.T) {
	port := &recoveringPort{events: make(chan Event, 1)}
	host := New(&recoverFactory{port: port}, nil)
	if err := host.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	port.events <- Event{Type: "modem.reset", Data: map[string]any{"reason": "reset"}}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && port.reconnects == 0 {
		time.Sleep(time.Millisecond)
	}
	if port.reconnects == 0 {
		t.Fatal("modem reset did not trigger recovery")
	}
	if err := host.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	port.events <- Event{Type: "network.changed"}
	time.Sleep(20 * time.Millisecond)
	if port.reconnects != 1 {
		t.Fatalf("disabled host recovered unexpectedly: reconnects=%d", port.reconnects)
	}
}

func TestHostRecoversAfterSIMNetworkAndExpiredCommandEvents(t *testing.T) {
	port := &recoveringPort{events: make(chan Event, 8)}
	host := New(&recoverFactory{port: port}, nil)
	if err := host.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index, eventType := range []string{"sim.changed", "network.changed", "command.expired", "modem.reset"} {
		port.events <- Event{Type: eventType}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && port.reconnects <= index {
			time.Sleep(time.Millisecond)
		}
		if port.reconnects <= index {
			t.Fatalf("event %s did not trigger recovery", eventType)
		}
	}
}

type recoverFactory struct{ port *recoveringPort }

func (f *recoverFactory) Open(context.Context) (backend.VoWiFiPort, error) { return f.port, nil }

func waitForHostEvent(t *testing.T, events <-chan runtime.Event, expected string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == expected {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", expected)
		}
	}
}
