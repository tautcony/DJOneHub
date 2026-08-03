package native

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
)

type fakeDriver struct {
	mu         sync.Mutex
	started    bool
	stopped    int
	events     []map[string]any
	startCalls int
}

func (d *fakeDriver) start(configJSON string, bridge *Bridge) {
	d.mu.Lock()
	d.started = true
	d.startCalls++
	d.mu.Unlock()
}

func (d *fakeDriver) handleEvent(eventJSON string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var event map[string]any
	_ = json.Unmarshal([]byte(eventJSON), &event)
	d.events = append(d.events, event)
}

func (d *fakeDriver) stop() {
	d.mu.Lock()
	d.stopped++
	d.mu.Unlock()
}

func (d *fakeDriver) hasUI() bool { return true }

func (d *fakeDriver) snapshot() (events []map[string]any, stopped int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]map[string]any(nil), d.events...), d.stopped
}

type recordingHandler struct {
	mu       sync.Mutex
	commands []notification.Command
}

func (h *recordingHandler) HandleCommand(_ context.Context, command notification.Command) {
	h.mu.Lock()
	h.commands = append(h.commands, command)
	h.mu.Unlock()
}

func (h *recordingHandler) snapshot() []notification.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]notification.Command(nil), h.commands...)
}

func waitForEvents(t *testing.T, driver *fakeDriver, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		events, _ := driver.snapshot()
		if len(events) >= count {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d events, got %d", count, len(events))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitForCommands(t *testing.T, handler *recordingHandler, count int) []notification.Command {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		commands := handler.snapshot()
		if len(commands) >= count {
			return commands
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d commands, got %d", count, len(commands))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestBridgeSinkEventsForwardedAsJSON(t *testing.T) {
	driver := &fakeDriver{}
	handler := &recordingHandler{}
	bridge := newWithDriver(handler, driver)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx, "http://127.0.0.1:7575/"); err != nil {
		t.Fatalf("start: %v", err)
	}

	call := notification.CallEvent{ID: "call-1", Direction: "incoming", State: "incoming", Number: "18900007376", StartedAt: time.Now().UTC()}
	bridge.ShowCall(call)
	bridge.ShowSMS(notification.SMSMessageEvent{Index: 7, Sender: "10086", Body: "验证码", Code: "482913", ReceivedAt: time.Now().UTC()})

	events := waitForEvents(t, driver, 2)
	if events[0]["type"] != notification.EventCallIncoming {
		t.Errorf("event 0 type = %v", events[0]["type"])
	}
	if events[1]["type"] != notification.EventSMSReceived {
		t.Errorf("event 1 type = %v", events[1]["type"])
	}
	if events[0]["version"] != float64(notification.EventVersion) {
		t.Errorf("version = %v", events[0]["version"])
	}
	data, ok := events[0]["data"].(map[string]any)
	if !ok || data["id"] != "call-1" {
		t.Errorf("event data = %v", events[0]["data"])
	}
	bridge.Stop()
	if _, stopped := driver.snapshot(); stopped != 1 {
		t.Errorf("stopped = %d, want 1", stopped)
	}
}

func TestBridgeCommandRoutingValidatesAndDispatches(t *testing.T) {
	driver := &fakeDriver{}
	handler := &recordingHandler{}
	bridge := newWithDriver(handler, driver)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx, ""); err != nil {
		t.Fatalf("start: %v", err)
	}

	bridge.enqueueCommand(`{"name":"reject_call","params":{"call_id":"call-1"}}`)
	bridge.enqueueCommand(`{"name":"open_dashboard"}`)
	// Invalid commands are dropped.
	bridge.enqueueCommand(`{"name":"reject_call"}`)
	bridge.enqueueCommand(`{"name":"unknown"}`)
	bridge.enqueueCommand(`not json`)

	commands := waitForCommands(t, handler, 2)
	if commands[0].Name != notification.CommandRejectCall || commands[0].CallID() != "call-1" {
		t.Errorf("command 0 = %+v", commands[0])
	}
	if commands[1].Name != notification.CommandOpenDashboard {
		t.Errorf("command 1 = %+v", commands[1])
	}
	time.Sleep(10 * time.Millisecond)
	if got := handler.snapshot(); len(got) != 2 {
		t.Errorf("invalid commands must be dropped, got %d", len(got))
	}
}

func TestBridgeWithoutHandlerDropsCommands(t *testing.T) {
	driver := &fakeDriver{}
	bridge := newWithDriver(nil, driver)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx, ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	bridge.enqueueCommand(`{"name":"open_dashboard"}`)
	time.Sleep(10 * time.Millisecond)
	bridge.Stop()
}

func TestBridgeStubReadyNeverCloses(t *testing.T) {
	// On darwin+cgo this exercises the real driver start via Start; on other
	// platforms the stub keeps Ready open forever.
	bridge := New(nil)
	select {
	case <-bridge.Ready():
		// Real UI only; nothing to assert here.
	default:
	}
	bridge.Stop()
}
