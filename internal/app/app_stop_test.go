package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/operation"
)

// TestAppStopClosesAdmissionCancelsOperationsAndClosesStoreLast drives the
// full shutdown sequence on an offline app: admission closes first, in-flight
// operations are cancelled and joined, workers stop in reverse start order,
// and the store is closed last (writes fail only after Stop returns).
func TestAppStopClosesAdmissionCancelsOperationsAndClosesStoreLast(t *testing.T) {
	// Redirect the user config dir so the app opens its SQLite store in a
	// temp directory instead of the real one.
	t.Setenv("HOME", t.TempDir())
	instance, err := NewOffline()
	if err != nil {
		t.Fatalf("NewOffline: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instance.Start(ctx)

	// While running, the HTTP server admits requests.
	admissionStatus := func() int {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/device", nil)
		recorder := httptest.NewRecorder()
		instance.HTTP.Handler().ServeHTTP(recorder, request)
		return recorder.Code
	}
	if status := admissionStatus(); status == http.StatusServiceUnavailable {
		t.Fatalf("requests refused before shutdown: %d", status)
	}

	// An operation starts and is cancelled+joined by Stop.
	started := make(chan struct{})
	id, err := instance.Operations.Start(ctx, "test.flash", func(taskCtx context.Context, _ func(int, string)) error {
		close(started)
		<-taskCtx.Done()
		return taskCtx.Err()
	})
	if err != nil {
		t.Fatalf("operation start: %v", err)
	}
	<-started

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := instance.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Admission is closed: new requests are refused.
	if status := admissionStatus(); status != http.StatusServiceUnavailable {
		t.Fatalf("requests admitted after shutdown: %d", status)
	}
	// The operation was cancelled and reports the cancelled terminal state.
	status, ok := instance.Operations.Get(id)
	if !ok {
		t.Fatalf("operation %s missing after shutdown", id)
	}
	if status.State != operation.Cancelled {
		t.Fatalf("operation state = %s, want cancelled", status.State)
	}
	// New operations are refused with the structured error and no ID.
	lateID, err := instance.Operations.Start(context.Background(), "late", func(context.Context, func(int, string)) error { return nil })
	if err == nil || !errors.Is(err, operation.ErrShutdown) {
		t.Fatalf("Start after shutdown = (%q, %v), want ErrShutdown", lateID, err)
	}
	// The store closed last: a write after Stop fails.
	namespace := instance.Store.Namespace("after_stop")
	value := map[string]string{"state": "written-after-stop"}
	if err := namespace.Write(&value); err == nil {
		t.Fatal("store write after Stop must fail")
	}
}

// TestAppStopTwiceIsSafe verifies Stop is idempotent enough to be called again
// (the caller runs the bounded sequence exactly once, but repeated calls must
// not panic or double-close the store).
func TestAppStopTwiceIsSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	instance, err := NewOffline()
	if err != nil {
		t.Fatalf("NewOffline: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instance.Start(ctx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := instance.Stop(stopCtx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// A second Stop must not double-close the store; the pollers already
	// joined, so each stop is a no-op.
	if err := instance.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
