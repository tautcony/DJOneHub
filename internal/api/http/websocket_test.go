package httpapi

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iniwex5/vohive/internal/application/firmware"
	domain "github.com/iniwex5/vohive/internal/domain/device"
)

func dialEvents(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+strings.TrimPrefix(ts.URL, "http://")+"/api/v1/events/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestWebSocketInitialSnapshotIncludesPublicEDLSession(t *testing.T) {
	discovery := &fakeReadyDiscovery{candidate: domain.Candidate{Identity: domain.Identity{StableID: "ws-edl", PhysicalLocation: "usb/1-2"}}}
	server, ops := newReadyServerWithBackend(t, discovery, &contractBackend{caps: allContractCapabilities()})
	server.config.DeviceControl = firmware.NewService(nil, ops, server.config.Runtime, firmware.Config{})
	_, err := server.config.Runtime.EDLSessions().Observe("usb/1-2", domain.EDLObservation{
		State: domain.EDLStateSaharaIdentified, SerialNumber: "12345678", HardwareID: "0102030405060708",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)
	conn := dialEvents(t, ts)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Type string `json:"type"`
		Data struct {
			EDLSession *domain.EDLSessionSnapshot `json:"edl_session"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "snapshot" || envelope.Data.EDLSession == nil {
		t.Fatalf("initial frame=%s", payload)
	}
	observation := envelope.Data.EDLSession.Observation
	if envelope.Data.EDLSession.PhysicalLocation != "" || observation.SerialNumber != "****5678" || observation.HardwareID != "****0708" {
		t.Fatalf("public EDL session=%+v", envelope.Data.EDLSession)
	}
}

func readFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) (id uint64, eventType string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var envelope struct {
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("bad envelope %s: %v", payload, err)
	}
	return envelope.ID, envelope.Type
}

// TestWebSocketSnapshotFirstWithWatermarkOrdering verifies the subscribe-with-
// watermark ordering: the session subscribes before the snapshot is sent, the
// snapshot carries the captured watermark as its ID, and every event delivered
// afterwards has a strictly greater ID, so client-side deduplication never
// discards a delivered event.
func TestWebSocketSnapshotFirstWithWatermarkOrdering(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)

	// A pre-existing event before the session: the watermark covers it.
	server.config.Runtime.Events().Publish("pre.session", nil)

	conn := dialEvents(t, ts)
	snapshotID, snapshotType := readFrame(t, conn, time.Second)
	if snapshotType != "snapshot" {
		t.Fatalf("first frame type = %q, want snapshot", snapshotType)
	}

	// Publish events while the snapshot was (or is being) constructed; every
	// delivered event must have ID > the snapshot's watermark.
	deadline := time.Now().Add(5 * time.Second)
	seen := 0
	for time.Now().Before(deadline) {
		server.config.Runtime.Events().Publish("post.session", seen)
		seen++
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if !errorsAsTimeout(err, &netErr) {
				t.Fatalf("read: %v", err)
			}
			continue
		}
		var envelope struct {
			ID   uint64 `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("bad envelope: %v", err)
		}
		if envelope.ID <= snapshotID {
			t.Fatalf("event id %d not greater than snapshot watermark %d", envelope.ID, snapshotID)
		}
		if envelope.Type == "post.session" {
			return
		}
	}
	t.Fatal("no post-session event delivered within deadline")
}

// waitForNoSubscribers waits until the server's event handler has fully
// exited (its unsubscribe ran), so the session's goroutine and subscription
// are released.
func waitForNoSubscribers(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(server.config.Runtime.Events().DropCounts().Active) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("event subscription never released")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestWebSocketStaleSessionIsReclaimed verifies a client that stops reading
// (and therefore never honors the keepalive) is closed by the read deadline,
// releasing its goroutine and event subscription.
func TestWebSocketStaleSessionIsReclaimed(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	// Shrink this server's keepalive windows so the read deadline fires fast.
	server.keepalive = websocketKeepalive{write: time.Second, pong: 300 * time.Millisecond, ping: 100 * time.Millisecond}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)

	conn := dialEvents(t, ts)
	// Drain the initial snapshot, then stop reading: the client never answers
	// the keepalive pings, so the server's read deadline fires and the
	// session is closed.
	readFrame(t, conn, time.Second)
	time.Sleep(700 * time.Millisecond)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("stale session must be closed by the server")
	}
	// The event subscription was released: the bus has no active subscribers.
	waitForNoSubscribers(t, server)
}

// TestWebSocketPingAndEventWritesDoNotRace drives concurrent event and ping
// writes (shrunk ping period) so the race detector (run under -race) can
// verify all writes go through the single writer.
func TestWebSocketPingAndEventWritesDoNotRace(t *testing.T) {
	server, _ := newReadyServer(t, allContractCapabilities())
	// Shrink this server's ping period so pings interleave with event writes.
	server.keepalive = websocketKeepalive{write: time.Second, pong: 2 * time.Second, ping: 10 * time.Millisecond}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	server.SetLoopbackPort(ts.Listener.Addr().(*net.TCPAddr).Port)

	conn := dialEvents(t, ts)
	readDone := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			select {
			case <-stop:
				_ = conn.Close()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	// Publish events while the server pings on the shrunk interval. All conn
	// operations happen in the reader goroutine, so the client side cannot
	// race Close against ReadMessage.
	for i := 0; i < 30; i++ {
		server.config.Runtime.Events().Publish("race.check", i)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-readDone
	// The server handler must exit (releasing its subscription) before the
	// keepalive windows are restored by the deferred call.
	waitForNoSubscribers(t, server)
}

func errorsAsTimeout(err error, netErr *net.Error) bool {
	value, ok := err.(net.Error)
	if ok {
		*netErr = value
		return value.Timeout()
	}
	return false
}
