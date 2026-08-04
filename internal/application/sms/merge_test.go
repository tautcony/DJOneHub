package sms

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/storage"
)

func message(index int, sender, body string, received time.Time) backend.SMSMessage {
	return backend.SMSMessage{Index: index, Sender: sender, Body: body, ReceivedAt: received}
}

func TestMergeReturnsFreshIncrementalMessages(t *testing.T) {
	service := &Service{}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	// First load: everything is fresh, which the Refresh caller suppresses.
	first, firstFresh := service.merge([]backend.SMSMessage{message(1, "10086", "第一条", now), message(2, "10010", "第二条", now)}, "")
	if len(first) != 2 || len(firstFresh) != 2 {
		t.Fatalf("first load merged=%d fresh=%d", len(first), len(firstFresh))
	}

	// Second load: only the new message is fresh; known messages dedup.
	second, secondFresh := service.merge([]backend.SMSMessage{
		message(1, "10086", "第一条", now),
		message(2, "10010", "第二条", now),
		message(3, "10086", "新验证码 9999", now.Add(time.Minute)),
	}, "")
	if len(second) != 3 {
		t.Fatalf("merged count = %d, want 3", len(second))
	}
	if len(secondFresh) != 1 || secondFresh[0].Index != 3 {
		t.Fatalf("fresh = %+v, want only message 3", secondFresh)
	}

	// Reordered module reads still dedup by content key, not index.
	reordered, reorderedFresh := service.merge([]backend.SMSMessage{message(3, "10086", "新验证码 9999", now.Add(time.Minute))}, "")
	if len(reordered) != 3 || len(reorderedFresh) != 0 {
		t.Errorf("reordered read must dedup: merged=%d fresh=%d", len(reordered), len(reorderedFresh))
	}
}

func TestMergeTrimsCacheToLimit(t *testing.T) {
	service := &Service{}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	items := make([]backend.SMSMessage, 0, 550)
	for i := 0; i < 550; i++ {
		items = append(items, message(i, "10086", "msg", now.Add(time.Duration(i)*time.Second)))
	}
	merged, fresh := service.merge(items, "")
	if len(merged) != 500 {
		t.Errorf("merged = %d, want 500", len(merged))
	}
	if len(fresh) != 550 {
		t.Errorf("fresh = %d, want all 550 (trim is a cache concern)", len(fresh))
	}
}

func TestMergeSortsNewestFirst(t *testing.T) {
	service := &Service{}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	merged, _ := service.merge([]backend.SMSMessage{message(1, "a", "旧", now), message(2, "b", "新", now.Add(5*time.Minute))}, "")
	if merged[0].Index != 2 {
		t.Errorf("newest message must come first, got index %d", merged[0].Index)
	}
}

func TestRecordSentPersistsAndRestoresHistory(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "djonehub.sqlite3")
	store, err := storage.OpenSQLite(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(nil, nil, nil, store)
	service.recordSent(context.Background(), "13800138000", "hello")

	restored := NewService(nil, nil, nil, store)
	if len(restored.sent) != 1 || len(restored.cache) != 1 {
		t.Fatalf("restored sent=%d cache=%d, want one message", len(restored.sent), len(restored.cache))
	}
	message := restored.sent[0]
	if message.Sender != "" || message.Recipient != "13800138000" || message.Body != "hello" {
		t.Fatalf("restored message = %+v", message)
	}
}
