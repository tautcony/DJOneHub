package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/notify"
	appRuntime "github.com/iniwex5/vohive/internal/runtime"
)

func TestRuntimeTraceStreamPublishesPayloadFreeUpdates(t *testing.T) {
	server := newTestServer(t, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/api/v1/runtime/traces/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(response.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if line == "\n" {
			break
		}
	}
	event := server.config.Runtime.Events().Publish("sms.received", map[string]any{"body": "must-not-stream"})
	server.config.Runtime.Events().RecordTraceHop(event.ID, "notification-policy", "handle", "success", "sms.received")
	server.config.Runtime.Events().RecordTraceHop(event.ID, "notification-queue", "enqueue", "success", "show_sms")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if strings.Contains(line, "must-not-stream") {
			t.Fatalf("stream leaked payload: %s", line)
		}
		var trace appRuntime.MessageTrace
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &trace); err != nil {
			t.Fatal(err)
		}
		if trace.Type != "sms.received" || len(trace.Hops) != 4 {
			t.Fatalf("trace = %#v", trace)
		}
		return
	}
	t.Fatal("timed out waiting for trace update")
}

func TestRuntimeTopologyShowsRecoveringNotificationChannel(t *testing.T) {
	topology := runtimeTopology(nil, nil, notify.Diagnostics{
		ConfiguredChannels: []string{"telegram"},
		Recovering: []notify.ChannelRecovery{{
			Channel: "telegram", Attempts: 2, Retryable: true,
			NextRetry: time.Now().UTC().Add(time.Minute), LastError: "EOF",
		}},
	})
	for _, node := range topology.Nodes {
		if node.ID == "telegram" {
			if node.State != "recovering" || !strings.Contains(node.Detail, "attempt 2") {
				t.Fatalf("telegram node = %#v", node)
			}
			return
		}
	}
	t.Fatal("telegram topology node missing")
}
