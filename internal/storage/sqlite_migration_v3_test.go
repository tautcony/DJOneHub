package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// createV2Database 构造一个真实 v2 数据库 (含旧表级唯一约束与 v1/v2 迁移记录)。
func createV2Database(t *testing.T, path string, seed func(*sql.DB) error) {
	t.Helper()
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version, applied_at) VALUES(1, '2026-08-03T00:00:00Z');
		INSERT INTO schema_migrations(version, applied_at) VALUES(2, '2026-08-04T00:00:00Z');
		CREATE TABLE sms_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
			provider_id INTEGER, sender TEXT NOT NULL DEFAULT '', recipient TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL, received_at TEXT NOT NULL, iccid TEXT NOT NULL DEFAULT '',
			concat_ref INTEGER, part_number INTEGER, total_parts INTEGER, created_at TEXT NOT NULL,
			UNIQUE (direction, sender, recipient, body, received_at)
		);
		CREATE INDEX idx_sms_messages_received_at ON sms_messages(received_at DESC);
		CREATE INDEX idx_sms_messages_peer_time ON sms_messages(sender, recipient, received_at DESC);
		CREATE TABLE call_records (
			id TEXT PRIMARY KEY, call_index INTEGER NOT NULL, direction TEXT NOT NULL,
			state TEXT NOT NULL, number TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL, ended_at TEXT, missed INTEGER NOT NULL DEFAULT 0,
			iccid TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		);
		CREATE TABLE app_settings (
			namespace TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at TEXT NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		if err := seed(legacy); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSQLiteMigratesV2ToV3PreservesRowsAndIDs 验证 v2 → v3 真实升级:
// 旧表约束被替换为含 SIM 身份的键, 行与 ID 全部保留。
func TestSQLiteMigratesV2ToV3PreservesRowsAndIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.sqlite3")
	createV2Database(t, path, func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO sms_messages(direction, provider_id, sender, body, received_at, iccid, created_at)
			VALUES
				('inbound', 7, '10086', '旧消息', '2026-08-03T12:00:00Z', '8901', '2026-08-03T12:00:05Z'),
				('inbound', 8, '10010', '旧消息二', '2026-08-03T13:00:00Z', '', '2026-08-03T13:00:05Z')
		`)
		return err
	})

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var version int
	if err := store.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != migrationVersion {
		t.Fatalf("schema version = %d, want %d", version, migrationVersion)
	}

	records, err := store.ListSMS("inbound", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v, want both rows preserved", records)
	}
	// ID 保留 (先插入的行 id 更小)。连接池单连接: 必须显式关闭 rows,
	// 否则后续 InsertSMS 永远拿不到连接。
	rows, err := store.db.Query(`SELECT id, body, iccid FROM sms_messages ORDER BY id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	var firstID, secondID int64
	var firstBody, secondBody, firstICCID, secondICCID string
	if !rows.Next() {
		t.Fatal("missing first row")
	}
	if err := rows.Scan(&firstID, &firstBody, &firstICCID); err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing second row")
	}
	if err := rows.Scan(&secondID, &secondBody, &secondICCID); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if firstID != 1 || secondID != 2 || firstBody != "旧消息" || secondBody != "旧消息二" {
		t.Fatalf("ids/data not preserved: (%d,%q) (%d,%q)", firstID, firstBody, secondID, secondBody)
	}
	if firstICCID != "8901" || secondICCID != "" {
		t.Fatalf("iccid values changed: %q %q", firstICCID, secondICCID)
	}

	// 新唯一键生效: 内容相同但 SIM 身份不同的两条消息都被存储。
	receivedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, iccid := range []string{"8901", "8902"} {
		if err := store.InsertSMS(SMSRecord{
			Direction: "inbound", Sender: "10086", Body: "相同内容", ReceivedAt: receivedAt,
			RecordedAt: receivedAt, ICCID: iccid,
		}); err != nil {
			t.Fatal(err)
		}
	}
	records, err = store.ListSMS("inbound", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	crossSIM := 0
	for _, record := range records {
		if record.Body == "相同内容" {
			crossSIM++
		}
	}
	if crossSIM != 2 {
		t.Fatalf("identical message on two SIMs stored %d times, want 2", crossSIM)
	}
}

// TestSQLiteV3MigrationRollsBackOnFailure 验证注入失败时 v3 迁移整体回滚:
// 旧表、数据与版本记录保持不变。
func TestSQLiteV3MigrationRollsBackOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2-rollback.sqlite3")
	createV2Database(t, path, func(db *sql.DB) error {
		_, err := db.Exec(`
			INSERT INTO sms_messages(direction, provider_id, sender, body, received_at, iccid, created_at)
			VALUES('inbound', 9, '10086', '回滚消息', '2026-08-03T12:00:00Z', '8901', '2026-08-03T12:00:05Z')
		`)
		return err
	})

	// 注入失败: 预先占用 sms_messages_v3 名字, 使迁移事务内的 CREATE TABLE
	// 必然失败 → 整体回滚。
	inject, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inject.Exec(`CREATE VIEW sms_messages_v3 AS SELECT id FROM sms_messages`); err != nil {
		t.Fatal(err)
	}
	_ = inject.Close()

	store, err := OpenSQLite(path)
	if err == nil {
		// 迁移失败不应返回打开成功; 若成功则断言版本未推进。
		defer store.Close()
		var version int
		if err := store.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 2 {
			t.Fatalf("schema version = %d after failed migration, want 2", version)
		}
		records, err := store.ListSMS("inbound", 0, 0)
		if err != nil {
			t.Fatalf("ListSMS() error = %v", err)
		}
		if len(records) != 1 || records[0].Body != "回滚消息" {
			t.Fatalf("rows lost after rollback: %+v", records)
		}
		return
	}

	// OpenSQLite 失败: 重新打开数据库确认旧数据仍在, 迁移未被记录, 旧表仍在。
	check, openErr := sql.Open("sqlite", path)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer check.Close()
	var version int
	if err := check.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d after failed migration, want 2", version)
	}
	var body string
	if err := check.QueryRow(`SELECT body FROM sms_messages WHERE provider_id = 9`).Scan(&body); err != nil {
		t.Fatalf("row lost after rollback: %v", err)
	}
	if body != "回滚消息" {
		t.Fatalf("row body = %q after rollback", body)
	}
	var tableName string
	if err := check.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'sms_messages'`).Scan(&tableName); err != nil {
		t.Fatalf("old sms_messages table missing after rollback: %v", err)
	}
}

// TestSQLiteListSMSPaginates 验证有界分页: 默认页大小生效, offset 分页有序。
func TestSQLiteListSMSPaginates(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		record := SMSRecord{
			Direction: "inbound", Sender: fmt.Sprintf("1008%d", i), Body: fmt.Sprintf("msg-%d", i),
			ReceivedAt: base.Add(time.Duration(i) * time.Minute), RecordedAt: base.Add(time.Duration(i) * time.Minute),
			ICCID: "8901",
		}
		if err := store.InsertSMS(record); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := store.ListSMS("inbound", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || page1[0].Body != "msg-4" || page1[1].Body != "msg-3" {
		t.Fatalf("page1 = %+v", page1)
	}
	page2, err := store.ListSMS("inbound", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].Body != "msg-2" || page2[1].Body != "msg-1" {
		t.Fatalf("page2 = %+v", page2)
	}
	page3, err := store.ListSMS("inbound", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || page3[0].Body != "msg-0" {
		t.Fatalf("page3 = %+v", page3)
	}
	// 非正 limit 使用有界默认值, 不返回全表。
	all, err := store.ListSMS("inbound", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("default-limit page = %d records, want 5", len(all))
	}
}
