package operation

import (
	"context"
	"errors"
	"testing"
	"time"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestManagerPublishesProgressAndCompletion(t *testing.T) {
	manager := NewManager(nil)
	_, events, unsubscribe := manager.bus.Subscribe(16)
	defer unsubscribe()
	id := manager.Start(context.Background(), "test", func(_ context.Context, progress func(int, string)) error { progress(50, "halfway"); return nil })
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
	id := manager.Start(ctx, "cancel", func(ctx context.Context, _ func(int, string)) error { <-ctx.Done(); return ctx.Err() })
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
	id := manager.Start(context.Background(), "error", func(context.Context, func(int, string)) error {
		return derrors.New(derrors.CapabilityNotSupported, "unsupported", false, map[string]any{"capability": "raw_at"})
	})
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
	t.Fatal(errors.New("operation did not fail"))
}
