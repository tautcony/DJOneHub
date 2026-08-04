package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteValueStoreRoundTrip(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	settings := store.Namespace("test")
	if err := settings.Write(map[string]string{"device": "slot-1"}); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := settings.Read(&value); err != nil {
		t.Fatal(err)
	}
	if value["device"] != "slot-1" {
		t.Fatalf("value = %#v", value)
	}
}

func TestSQLiteSMSRoundTripAndDeduplication(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	receivedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	record := SMSRecord{Direction: "outbound", ProviderID: -1, Recipient: "10001", Body: "hello", ReceivedAt: receivedAt}
	if err := store.InsertSMS(record); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSMS(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListSMS("outbound")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Body != "hello" || !records[0].ReceivedAt.Equal(receivedAt) {
		t.Fatalf("records = %+v", records)
	}
}

func TestSQLiteCallRoundTripAndUpdate(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	startedAt := time.Date(2026, 8, 4, 0, 30, 0, 0, time.UTC)
	updatedAt := startedAt.Add(45 * time.Second)
	endedAt := startedAt.Add(60 * time.Second)
	record := CallRecord{
		ID: "call-1", Index: 1, Direction: "incoming", State: "active", Number: "1502",
		StartedAt: startedAt, UpdatedAt: updatedAt,
	}
	if err := store.InsertCall(record); err != nil {
		t.Fatal(err)
	}
	record.State, record.UpdatedAt, record.EndedAt, record.Missed = "incoming", endedAt, &endedAt, true
	if err := store.InsertCall(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListCalls(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].State != "incoming" || records[0].Number != "1502" || !records[0].Missed || records[0].EndedAt == nil || !records[0].EndedAt.Equal(endedAt) {
		t.Fatalf("records = %+v", records)
	}
}

// TestSQLiteMigratesV1ToV2WithICCID builds a v1 database by hand and checks
// that opening it adds the iccid columns and keeps the pre-existing rows.
func TestSQLiteMigratesV1ToV2WithICCID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite3")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version, applied_at) VALUES(1, '2026-08-03T00:00:00Z');
		CREATE TABLE sms_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
			provider_id INTEGER, sender TEXT NOT NULL DEFAULT '', recipient TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL, received_at TEXT NOT NULL, concat_ref INTEGER, part_number INTEGER,
			total_parts INTEGER, created_at TEXT NOT NULL,
			UNIQUE (direction, sender, recipient, body, received_at)
		);
		CREATE TABLE call_records (
			id TEXT PRIMARY KEY, call_index INTEGER NOT NULL, direction TEXT NOT NULL,
			state TEXT NOT NULL, number TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL, ended_at TEXT, missed INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);
		INSERT INTO sms_messages(direction, provider_id, sender, body, received_at, created_at)
			VALUES('inbound', 7, '10086', '旧消息', '2026-08-03T12:00:00Z', '2026-08-03T12:00:05Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	records, err := store.ListSMS("inbound")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Body != "旧消息" || records[0].ICCID != "" {
		t.Fatalf("migrated sms records = %+v", records)
	}

	receivedAt := time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC)
	if err := store.InsertSMS(SMSRecord{
		Direction: "inbound", Sender: "10086", Body: "新消息", ReceivedAt: receivedAt,
		RecordedAt: receivedAt.Add(time.Second), ICCID: "8901",
	}); err != nil {
		t.Fatal(err)
	}
	records, err = store.ListSMS("inbound")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ICCID != "8901" {
		t.Fatalf("records after v2 write = %+v", records)
	}

	startedAt := time.Date(2026, 8, 4, 0, 30, 0, 0, time.UTC)
	if err := store.InsertCall(CallRecord{
		ID: "call-1", Index: 1, Direction: "incoming", State: "active", Number: "1502",
		StartedAt: startedAt, UpdatedAt: startedAt, ICCID: "8901",
	}); err != nil {
		t.Fatal(err)
	}
	calls, err := store.ListCalls(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ICCID != "8901" {
		t.Fatalf("call records = %+v", calls)
	}
}

func TestSQLiteTrafficDailyUsesCounterDeltasAndICCIDs(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := time.Date(2026, 8, 4, 8, 0, 0, 0, time.Local)
	second := first.Add(10 * time.Second)
	record, err := store.RecordTrafficSample("8901", first, 1_000, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if record.RXBytes != 0 || record.TXBytes != 0 {
		t.Fatalf("first sample should establish a baseline: %+v", record)
	}
	record, err = store.RecordTrafficSample("8901", second, 1_250, 2_500)
	if err != nil {
		t.Fatal(err)
	}
	if record.RXBytes != 250 || record.TXBytes != 500 {
		t.Fatalf("daily delta = %+v", record)
	}

	other, err := store.RecordTrafficSample("8902", second, 9_000, 9_000)
	if err != nil {
		t.Fatal(err)
	}
	if other.RXBytes != 0 || other.TXBytes != 0 {
		t.Fatalf("different ICCID must have an independent baseline: %+v", other)
	}
	found, ok, err := store.TrafficDaily("8901", first.Format("2006-01-02"))
	if err != nil || !ok || found.RXBytes != 250 || found.TXBytes != 500 {
		t.Fatalf("stored daily row = %+v, found=%v, err=%v", found, ok, err)
	}
}
