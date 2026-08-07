package storage

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNotificationHistoryUpsertAndList(t *testing.T) {
	store := openTestStore(t)

	if err := store.UpsertNotificationHistory(NotificationHistoryRecord{
		SequenceNumber: 13,
		Event:          "enable",
		ICCID:          "8986012001000000000",
		Address:        "smdp.example.com",
		State:          NotificationStatePending,
	}); err != nil {
		t.Fatalf("upsert pending: %v", err)
	}

	// 同键更新状态：首次观察时间保持，状态与更新时间变化。
	if err := store.UpsertNotificationHistory(NotificationHistoryRecord{
		SequenceNumber: 13,
		Event:          "enable",
		ICCID:          "8986012001000000000",
		State:          NotificationStateProcessed,
	}); err != nil {
		t.Fatalf("upsert processed: %v", err)
	}

	if err := store.UpsertNotificationHistory(NotificationHistoryRecord{
		SequenceNumber: 14,
		Event:          "disable",
		ICCID:          "8986012001000000000",
		State:          NotificationStatePending,
	}); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	records, err := store.ListNotificationHistory(0)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("history count = %d, want 2", len(records))
	}
	// 排序键是 updated_at（同刻按 sequence DESC），不断言顺序，只断言内容。
	bySeq := map[int64]NotificationHistoryRecord{}
	for _, record := range records {
		bySeq[record.SequenceNumber] = record
	}
	if record, ok := bySeq[13]; !ok || record.State != NotificationStateProcessed {
		t.Fatalf("seq 13 record = %+v, want processed", record)
	}
	if record, ok := bySeq[14]; !ok || record.State != NotificationStatePending {
		t.Fatalf("seq 14 record = %+v, want pending", record)
	}
	for _, record := range records {
		if record.ObservedAt.IsZero() || record.UpdatedAt.Before(record.ObservedAt) {
			t.Fatalf("timestamps invalid: observed=%v updated=%v", record.ObservedAt, record.UpdatedAt)
		}
	}
}

func TestNotificationHistoryUpsertRequiresKey(t *testing.T) {
	store := openTestStore(t)
	if err := store.UpsertNotificationHistory(NotificationHistoryRecord{Event: "enable"}); err == nil {
		t.Fatal("upsert without sequence must fail")
	}
	if err := store.UpsertNotificationHistory(NotificationHistoryRecord{SequenceNumber: 1}); err == nil {
		t.Fatal("upsert without event must fail")
	}
}

func TestMarkNotificationHistoryAbsent(t *testing.T) {
	store := openTestStore(t)
	for _, record := range []NotificationHistoryRecord{
		{SequenceNumber: 1, Event: "enable", ICCID: "a", State: NotificationStatePending},
		{SequenceNumber: 2, Event: "disable", ICCID: "a", State: NotificationStatePending},
		{SequenceNumber: 3, Event: "install", ICCID: "b", State: NotificationStatePending},
	} {
		if err := store.UpsertNotificationHistory(record); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	// 当前快照只有 seq 1 和 seq 2：seq 3 应被标记 processed，其余保持 pending。
	current := []NotificationHistoryRecord{
		{SequenceNumber: 1, Event: "enable", ICCID: "a"},
		{SequenceNumber: 2, Event: "disable", ICCID: "a"},
	}
	if err := store.MarkNotificationHistoryAbsent(current); err != nil {
		t.Fatalf("mark absent: %v", err)
	}

	records, err := store.ListNotificationHistory(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	bySeq := map[int64]NotificationHistoryState{}
	for _, record := range records {
		bySeq[record.SequenceNumber] = record.State
	}
	if bySeq[1] != NotificationStatePending || bySeq[2] != NotificationStatePending {
		t.Fatalf("present records must stay pending: %v", bySeq)
	}
	if bySeq[3] != NotificationStateProcessed {
		t.Fatalf("absent record must become processed: %v", bySeq)
	}

	// 空快照：全部 pending 都标记 processed。
	current = nil
	if err := store.MarkNotificationHistoryAbsent(current); err != nil {
		t.Fatalf("mark absent empty: %v", err)
	}
	records, err = store.ListNotificationHistory(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, record := range records {
		if record.State != NotificationStateProcessed {
			t.Fatalf("record %d state = %s, want processed", record.SequenceNumber, record.State)
		}
	}
}

func TestNotificationHistorySchemaVersion(t *testing.T) {
	store := openTestStore(t)
	var version int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != migrationVersion {
		t.Fatalf("schema version = %d, want %d", version, migrationVersion)
	}
}
