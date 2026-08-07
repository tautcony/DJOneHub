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
	// 设备身份未知: 消息保留在内存发送缓存, 不落库 (design D16)。
	service.recordSent(context.Background(), "13800138000", "hello")
	if len(service.sent) != 1 || len(service.cache) != 1 {
		t.Fatalf("in-memory sent=%d cache=%d, want one message", len(service.sent), len(service.cache))
	}

	restored := NewService(nil, nil, nil, store)
	if len(restored.sent) != 0 || len(restored.cache) != 0 {
		t.Fatalf("message without identity must not be persisted; restored sent=%d cache=%d", len(restored.sent), len(restored.cache))
	}

	// 身份已知时持久化并可恢复。
	now := time.Now().UTC()
	if err := store.InsertSMS(storage.SMSRecord{
		Direction: "outbound", Recipient: "13800138000", Body: "hello",
		ReceivedAt: now, RecordedAt: now, ICCID: "8901",
	}); err != nil {
		t.Fatal(err)
	}
	restored2 := NewService(nil, nil, nil, store)
	if len(restored2.sent) != 1 || len(restored2.cache) != 1 {
		t.Fatalf("restored sent=%d cache=%d, want one message", len(restored2.sent), len(restored2.cache))
	}
	message := restored2.sent[0]
	if message.Sender != "" || message.Recipient != "13800138000" || message.Body != "hello" || message.ICCID != "8901" {
		t.Fatalf("restored message = %+v", message)
	}
}

func TestReloadPersistedMessagesRepairsMemoryCache(t *testing.T) {
	store, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if err := store.InsertSMS(storage.SMSRecord{
		Direction: "inbound", Sender: "10086", Body: "hello", ReceivedAt: now,
		RecordedAt: now, ICCID: "8901",
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, nil, store)
	service.cache[0].ICCID = ""

	if err := service.reloadPersistedMessages(); err != nil {
		t.Fatal(err)
	}
	if service.cache[0].ICCID != "8901" {
		t.Fatalf("reloaded ICCID = %q, want 8901", service.cache[0].ICCID)
	}
}
