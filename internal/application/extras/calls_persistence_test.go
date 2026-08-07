package extras

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
)

func TestCallHistoryRestoresFromSQLite(t *testing.T) {
	store, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	bus := runtime.NewEventBus()
	service := NewService(nil, operation.NewManager(bus), nil, store)
	startedAt := time.Date(2026, 8, 4, 0, 30, 0, 0, time.UTC)
	// The first snapshot is the silent startup baseline, but the archived
	// leftover call must still be persisted as history.
	service.applyCalls([]callCandidate{candidate(1, "incoming", "incoming", "1502")}, startedAt, "")
	service.applyCalls(nil, startedAt.Add(time.Minute), "")

	restored := NewService(nil, operation.NewManager(bus), nil, store)
	status := restored.Calls(context.Background())
	if len(status.History) != 1 {
		t.Fatalf("history = %+v", status.History)
	}
	if status.History[0].Number != "1502" || !status.History[0].Missed {
		t.Fatalf("record = %+v", status.History[0])
	}
}
