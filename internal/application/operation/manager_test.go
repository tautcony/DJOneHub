package operation

import (
	"context"
	stdErrors "errors"
	"testing"
	"time"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestManagerLogPreservesTerminalControlChunks(t *testing.T) {
	manager := NewManager(nil)
	_, events, unsubscribe := manager.bus.Subscribe(16)
	defer unsubscribe()
	id, err := manager.Start(context.Background(), "terminal", func(context.Context, string, func(int, string)) error {
		return nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	manager.Log(id, "\r")
	for {
		select {
		case event := <-events:
			if event.Type != "operation.log" {
				continue
			}
			logEntry := event.Data.(Log)
			if logEntry.Message != "\r" {
				t.Fatalf("log message = %q, want carriage return", logEntry.Message)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for operation log")
		}
	}
}

func TestManagerPublishesProgressAndCompletion(t *testing.T) {
	manager := NewManager(nil)
	_, events, unsubscribe := manager.bus.Subscribe(16)
	defer unsubscribe()
	id, err := manager.Start(context.Background(), "test", func(_ context.Context, _ string, progress func(int, string)) error {
		progress(50, "halfway")
		return nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.After(time.Second)
	var completed bool
	for !completed {
		select {
		case event := <-events:
			if event.Type == "operation.completed" {
				status := event.Data.(Status)
				if status.ID != id || status.State != Succeeded || status.Progress != 100 {
					t.Fatalf("status = %+v", status)
				}
				completed = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for completion")
		}
	}
}

func TestManagerCancellation(t *testing.T) {
	manager := NewManager(nil)
	ctx := context.Background()
	id, err := manager.Start(ctx, "cancel", func(ctx context.Context, _ string, _ func(int, string)) error { <-ctx.Done(); return ctx.Err() })
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if !manager.Cancel(id) {
		t.Fatal("operation was not cancellable")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := manager.Get(id)
		if ok && status.State == Cancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not become cancelled")
}

func TestManagerPreservesStructuredErrors(t *testing.T) {
	manager := NewManager(nil)
	id, err := manager.Start(context.Background(), "error", func(context.Context, string, func(int, string)) error {
		return derrors.New(derrors.CapabilityNotSupported, "unsupported", false, map[string]any{"capability": "raw_at"})
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := manager.Get(id)
		if ok && status.State == Failed {
			if status.Error == nil || status.Error.Code != derrors.CapabilityNotSupported {
				t.Fatalf("error = %+v", status.Error)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(stdErrors.New("operation did not fail"))
}

func TestStartAfterShutdownReturnsStructuredError(t *testing.T) {
	manager := NewManager(nil)
	ctx := context.Background()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	id, err := manager.Start(ctx, "late", func(context.Context, string, func(int, string)) error { return nil })
	if err == nil {
		t.Fatalf("Start after shutdown must fail, got id %q", id)
	}
	if !stdErrors.Is(err, ErrShutdown) {
		t.Fatalf("Start error = %v, want ErrShutdown", err)
	}
	if id != "" {
		t.Fatalf("Start after shutdown returned id %q, want empty", id)
	}
}

func TestShutdownCancelsRunningOperations(t *testing.T) {
	manager := NewManager(nil)
	id, err := manager.Start(context.Background(), "flash", func(ctx context.Context, _ string, _ func(int, string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Let the worker reach the waiting state.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if status, ok := manager.Get(id); ok && status.State == Running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	status, ok := manager.Get(id)
	if !ok {
		t.Fatalf("operation %s missing after shutdown", id)
	}
	if status.State != Cancelled {
		t.Fatalf("state = %s, want %s", status.State, Cancelled)
	}
}

func TestShutdownIsIdempotentAndTimeoutDoesNotPoisonLaterCallers(t *testing.T) {
	manager := NewManager(nil)
	// A long-running worker that ignores cancellation for a while.
	started := make(chan struct{})
	id, err := manager.Start(context.Background(), "slow", func(ctx context.Context, _ string, _ func(int, string)) error {
		close(started)
		select {
		case <-ctx.Done():
			time.Sleep(150 * time.Millisecond)
			return ctx.Err()
		case <-time.After(time.Second):
			return nil
		}
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started

	// The first caller times out before the worker joins.
	impatient, cancelImpatient := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelImpatient()
	if err := manager.Shutdown(impatient); err == nil {
		t.Fatal("first Shutdown must time out")
	}

	// A later caller with room waits for the same close signal and succeeds,
	// so an early caller timeout does not poison later shutdown waits.
	patient, cancelPatient := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPatient()
	if err := manager.Shutdown(patient); err != nil {
		t.Fatalf("later Shutdown: %v", err)
	}
	status, ok := manager.Get(id)
	if !ok || status.State != Cancelled {
		t.Fatalf("operation state = %+v, ok = %v; want cancelled", status, ok)
	}
}

func TestManagerHasActiveKind(t *testing.T) {
	manager := NewManager(nil)
	if manager.HasActiveKind() {
		t.Fatal("HasActiveKind() with no kinds = true, want false")
	}
	if manager.HasActiveKind("") {
		t.Fatal("HasActiveKind() with only blank kinds = true, want false")
	}
	if manager.HasActiveKind("esim.enable") {
		t.Fatal("HasActiveKind() before any operation = true, want false")
	}

	block := make(chan struct{})
	id, err := manager.Start(context.Background(), "esim.enable", func(context.Context, string, func(int, string)) error {
		<-block
		return nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer manager.Cancel(id)

	if !manager.HasActiveKind("esim.enable") {
		t.Fatal("HasActiveKind(esim.enable) during operation = false, want true")
	}
	if !manager.HasActiveKind("esim.disable", "esim.enable") {
		t.Fatal("HasActiveKind(multi) during operation = false, want true")
	}
	if manager.HasActiveKind("esim.disable") {
		t.Fatal("HasActiveKind(esim.disable) during esim.enable = true, want false")
	}

	close(block)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !manager.HasActiveKind("esim.enable") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HasActiveKind(esim.enable) still true after operation completed")
}
