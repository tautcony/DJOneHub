package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ValueStore is the small settings-store contract used by services that keep
// a typed JSON document. SQLiteStore implements it while keeping each
// document isolated by namespace.
type ValueStore interface {
	Read(any) error
	Write(any) error
}

// SQLiteStore owns one SQLite connection pool. Namespace returns a view over
// the same database for app_settings documents; SMS rows use the root view.
type SQLiteStore struct {
	db        *sql.DB
	namespace string
}

// SMSRecord is the storage representation of one message. The application
// layer maps it to and from backend.SMSMessage to keep storage independent of
// modem contracts. RecordedAt is persisted to the created_at column and is the
// single-clock ordering key; ReceivedAt is a display attribute.
type SMSRecord struct {
	Direction  string
	ProviderID int
	Sender     string
	Recipient  string
	Body       string
	ReceivedAt time.Time
	RecordedAt time.Time
	ICCID      string
	ConcatRef  int
	PartNumber int
	TotalParts int
}

// CallRecord is the storage representation of one completed call.
type CallRecord struct {
	ID          string
	Index       int
	Direction   string
	State       string
	Number      string
	StartedAt   time.Time
	UpdatedAt   time.Time
	EndedAt     *time.Time
	ConnectedAt *time.Time
	Missed      bool
	ICCID       string
}

// TrafficDailyRecord stores usage derived from network-interface counters for
// one ICCID and one local calendar day.
type TrafficDailyRecord struct {
	ICCID         string
	UsageDate     string
	RXBytes       uint64
	TXBytes       uint64
	LastRXCounter uint64
	LastTXCounter uint64
	LastSampledAt time.Time
}

type SimProfileType string

const (
	SimProfileUnknown  SimProfileType = "unknown"
	SimProfilePhysical SimProfileType = "physical"
	SimProfileESIM     SimProfileType = "esim"
)

// SimProfileRecord is the canonical ICCID-keyed subscription record. IMSI and
// MSISDN are device observations; Name, LocalPhone, Notes, and Tags are local
// user metadata and are never overwritten by observation.
type SimProfileRecord struct {
	ICCID       string
	IMSI        string
	MSISDN      string
	Name        string
	LocalPhone  string
	Notes       string
	Tags        string
	ProfileType SimProfileType
	FirstSeen   time.Time
	LastSeen    time.Time
	UpdatedAt   time.Time
}

// NotificationHistoryState 描述一条 eUICC 通知在其生命周期内的处置状态。
type NotificationHistoryState string

const (
	// NotificationStatePending 是首次在卡片上观察到但尚未处置。
	NotificationStatePending NotificationHistoryState = "pending"
	// NotificationStateProcessed 是已成功上报给 SM-DP+（自动清理或手动重发）。
	NotificationStateProcessed NotificationHistoryState = "processed"
	// NotificationStateFailed 是上报失败，仍保留在卡内。
	NotificationStateFailed NotificationHistoryState = "failed"
	// NotificationStateRemoved 是用户手动从卡片删除、未上报。
	NotificationStateRemoved NotificationHistoryState = "removed"
)

// NotificationHistoryRecord 是 eUICC 通知的历史记录。卡片上的通知被清理后
// 记录仍保留在本地库中，供用户查看处置轨迹。SequenceNumber 按卡片作用域递增，
// 去重键为 (sequence_number, iccid, event)，避免跨卡片混淆。
type NotificationHistoryRecord struct {
	SequenceNumber int64
	Event          string
	ICCID          string
	Address        string
	AID            string
	State          NotificationHistoryState
	ObservedAt     time.Time
	UpdatedAt      time.Time
}

// OpenSQLite creates or opens the single local application database and
// applies all schema migrations before returning it.
func OpenSQLite(path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db}
	if err := store.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) configure() error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS app_settings (
			namespace TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sms_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
			provider_id INTEGER,
			sender TEXT NOT NULL DEFAULT '',
			recipient TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			received_at TEXT NOT NULL,
			iccid TEXT NOT NULL DEFAULT '',
			concat_ref INTEGER,
			part_number INTEGER,
			total_parts INTEGER,
			created_at TEXT NOT NULL,
			UNIQUE (direction, sender, recipient, body, received_at)
		);
		CREATE INDEX IF NOT EXISTS idx_sms_messages_received_at
			ON sms_messages(received_at DESC);
		CREATE INDEX IF NOT EXISTS idx_sms_messages_peer_time
			ON sms_messages(sender, recipient, received_at DESC);
		CREATE TABLE IF NOT EXISTS call_records (
			id TEXT PRIMARY KEY,
			call_index INTEGER NOT NULL,
			direction TEXT NOT NULL,
			state TEXT NOT NULL,
			number TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			ended_at TEXT,
			connected_at TEXT,
			missed INTEGER NOT NULL DEFAULT 0,
			iccid TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_call_records_started_at
			ON call_records(started_at DESC);
		CREATE TABLE IF NOT EXISTS traffic_daily (
			iccid TEXT NOT NULL,
			usage_date TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL DEFAULT 0,
			tx_bytes INTEGER NOT NULL DEFAULT 0,
			last_rx_counter INTEGER NOT NULL,
			last_tx_counter INTEGER NOT NULL,
			last_sampled_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (iccid, usage_date)
		);
		CREATE INDEX IF NOT EXISTS idx_traffic_daily_date
			ON traffic_daily(usage_date DESC);
		CREATE TABLE IF NOT EXISTS esim_notification_history (
			sequence_number INTEGER NOT NULL,
			event TEXT NOT NULL,
			iccid TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			aid TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'pending',
			observed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (sequence_number, iccid, event)
		);
		CREATE INDEX IF NOT EXISTS idx_esim_notification_history_updated
			ON esim_notification_history(updated_at DESC);
		CREATE TABLE IF NOT EXISTS sim_profiles (
			iccid TEXT PRIMARY KEY,
			imsi TEXT NOT NULL DEFAULT '',
			msisdn TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			local_phone TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '',
			profile_type TEXT NOT NULL DEFAULT 'unknown'
				CHECK (profile_type IN ('unknown', 'physical', 'esim')),
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_sim_profiles_last_seen
			ON sim_profiles(last_seen_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	return s.applyMigrations()
}

// applyMigrations runs versioned ALTERs on top of the baseline schema. Every
// migration must be idempotent: a database created from the current CREATE
// TABLE statements already contains the columns it adds.
func (s *SQLiteStore) applyMigrations() error {
	var version int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema migration version: %w", err)
	}
	for version < migrationVersion {
		version++
		switch version {
		case 2:
			// v2: record the SIM identity alongside each message/call.
			for _, column := range []struct{ table, name, definition string }{
				{"sms_messages", "iccid", "iccid TEXT NOT NULL DEFAULT ''"},
				{"call_records", "iccid", "iccid TEXT NOT NULL DEFAULT ''"},
			} {
				if err := s.ensureColumn(column.table, column.name, column.definition); err != nil {
					return err
				}
			}
			if err := s.recordMigration(version); err != nil {
				return err
			}
		case 3:
			if err := s.migrateSMSUniquenessWithIdentity(); err != nil {
				return err
			}
		case 4:
			// v4: eUICC 通知历史表（基线 CREATE TABLE 已包含，幂等）。
			if err := s.ensureColumn("esim_notification_history", "state", "state TEXT NOT NULL DEFAULT 'pending'"); err != nil {
				return err
			}
			if err := s.recordMigration(version); err != nil {
				return err
			}
		case 5:
			// v5 used the legacy sim_cards table. A fresh database now creates
			// sim_profiles directly, so only alter the legacy table when present.
			exists, err := s.tableExists("sim_cards")
			if err != nil {
				return err
			}
			if exists {
				if err := s.ensureColumn("sim_cards", "notes", "notes TEXT NOT NULL DEFAULT ''"); err != nil {
					return err
				}
			}
			if err := s.recordMigration(version); err != nil {
				return err
			}
		case 6:
			if err := s.migrateSIMProfiles(); err != nil {
				return err
			}
		case 7:
			if err := s.ensureColumn("call_records", "connected_at", "connected_at TEXT"); err != nil {
				return err
			}
			if err := s.recordMigration(version); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrationVersion is the newest schema version this binary understands.
const migrationVersion = 7

func (s *SQLiteStore) recordMigration(version int) error {
	if _, err := s.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record sqlite schema migration: %w", err)
	}
	return nil
}

func (s *SQLiteStore) tableExists(table string) (bool, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	return exists, nil
}

type legacyProfileNote struct {
	Label string `json:"label"`
	Phone string `json:"phone"`
	Tags  string `json:"tags"`
}

// migrateSIMProfiles moves the legacy relational rows and JSON Profile notes
// into the canonical table in one transaction. The baseline schema creates an
// empty sim_profiles table before migrations, including after a failed attempt;
// all data changes and namespace removal still commit atomically here.
func (s *SQLiteStore) migrateSIMProfiles() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin sim profiles v6 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var legacyTable bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'sim_cards')`).Scan(&legacyTable); err != nil {
		return fmt.Errorf("inspect legacy sim cards table: %w", err)
	}
	if legacyTable {
		if _, err := tx.Exec(`
			INSERT INTO sim_profiles(
				iccid, imsi, msisdn, name, local_phone, notes, tags, profile_type,
				first_seen_at, last_seen_at, updated_at
			)
			SELECT iccid, imsi, msisdn, name, '', notes, '', 'unknown',
				first_seen_at, last_seen_at, updated_at
			FROM sim_cards
		`); err != nil {
			return fmt.Errorf("copy legacy sim cards: %w", err)
		}
	}

	var encoded string
	err = tx.QueryRow(`SELECT value_json FROM app_settings WHERE namespace = 'profile_notes'`).Scan(&encoded)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read legacy profile notes: %w", err)
	}
	if err == nil {
		var notes map[string]legacyProfileNote
		if decodeErr := json.Unmarshal([]byte(encoded), &notes); decodeErr != nil {
			return fmt.Errorf("decode legacy profile notes: %w", decodeErr)
		}
		for rawICCID, note := range notes {
			if err := migrateLegacyProfileNote(tx, rawICCID, note); err != nil {
				return err
			}
		}
	}

	if legacyTable {
		if _, err := tx.Exec(`DROP TABLE sim_cards`); err != nil {
			return fmt.Errorf("drop legacy sim cards: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM app_settings WHERE namespace = 'profile_notes'`); err != nil {
		return fmt.Errorf("remove legacy profile notes: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(6, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record sim profiles v6 migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sim profiles v6 migration: %w", err)
	}
	return nil
}

func migrateLegacyProfileNote(tx *sql.Tx, rawICCID string, note legacyProfileNote) error {
	iccid := strings.TrimSpace(rawICCID)
	if iccid == "" || len(iccid) > 22 {
		return fmt.Errorf("invalid ICCID in legacy profile notes")
	}
	note.Label = strings.TrimSpace(note.Label)
	note.Phone = strings.TrimSpace(note.Phone)
	note.Tags = strings.TrimSpace(note.Tags)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`
		INSERT INTO sim_profiles(
			iccid, name, local_phone, tags, profile_type, first_seen_at, last_seen_at, updated_at
		) VALUES(?, ?, ?, ?, 'esim', ?, ?, ?)
		ON CONFLICT(iccid) DO NOTHING
	`, iccid, note.Label, note.Phone, note.Tags, now, now, now); err != nil {
		return fmt.Errorf("insert legacy profile note %s: %w", iccid, err)
	}

	var name, localPhone, existingNotes, tags string
	if err := tx.QueryRow(`
		SELECT name, local_phone, notes, tags FROM sim_profiles WHERE iccid = ?
	`, iccid).Scan(&name, &localPhone, &existingNotes, &tags); err != nil {
		return fmt.Errorf("read migrated profile %s: %w", iccid, err)
	}
	if name == "" {
		name = note.Label
	} else if note.Label != "" && note.Label != name {
		conflict := "Migrated eSIM label: " + note.Label
		if existingNotes == "" {
			existingNotes = conflict
		} else if !strings.Contains(existingNotes, conflict) {
			existingNotes += "\n" + conflict
		}
	}
	if localPhone == "" {
		localPhone = note.Phone
	}
	if tags == "" {
		tags = note.Tags
	}
	if _, err := tx.Exec(`
		UPDATE sim_profiles SET name = ?, local_phone = ?, notes = ?, tags = ?,
			profile_type = 'esim', updated_at = ? WHERE iccid = ?
	`, name, localPhone, existingNotes, tags, now, iccid); err != nil {
		return fmt.Errorf("merge legacy profile note %s: %w", iccid, err)
	}
	return nil
}

// migrateSMSUniquenessWithIdentity 在单个事务中重建 sms_messages 表: 把表级
// 唯一约束从 (direction, sender, recipient, body, received_at) 替换为含 SIM
// 身份的 (direction, iccid, sender, recipient, body, received_at), 使同一
// 消息在第二张 SIM 上被存储而不是被 IGNORE。SQLite 无法原地删除表级约束,
// 因此: 建新表 → 复制全部行与 ID → 重建索引 → 换表 → 记录迁移。任一步失败
// 整体回滚, 旧表与数据保持不变; 版本记录只在换表成功后写入 (同一事务)。
func (s *SQLiteStore) migrateSMSUniquenessWithIdentity() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin sms v3 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []string{
		`CREATE TABLE sms_messages_v3 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
			provider_id INTEGER,
			sender TEXT NOT NULL DEFAULT '',
			recipient TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			received_at TEXT NOT NULL,
			iccid TEXT NOT NULL DEFAULT '',
			concat_ref INTEGER,
			part_number INTEGER,
			total_parts INTEGER,
			created_at TEXT NOT NULL,
			UNIQUE (direction, iccid, sender, recipient, body, received_at)
		)`,
		`INSERT INTO sms_messages_v3 (id, direction, provider_id, sender, recipient, body, received_at, iccid, concat_ref, part_number, total_parts, created_at)
			SELECT id, direction, provider_id, sender, recipient, body, received_at, iccid, concat_ref, part_number, total_parts, created_at FROM sms_messages`,
		`DROP TABLE sms_messages`,
		`ALTER TABLE sms_messages_v3 RENAME TO sms_messages`,
		`CREATE INDEX idx_sms_messages_received_at ON sms_messages(received_at DESC)`,
		`CREATE INDEX idx_sms_messages_peer_time ON sms_messages(sender, recipient, received_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate sms uniqueness to v3: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(3, ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record sms v3 migration: %w", err)
	}
	return tx.Commit()
}

// ensureColumn adds a column unless it already exists.
func (s *SQLiteStore) ensureColumn(table, name, definition string) error {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan %s column: %w", table, err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}
	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %s`, table, definition)); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, name, err)
	}
	return nil
}

func (s *SQLiteStore) Namespace(namespace string) *SQLiteStore {
	return &SQLiteStore{db: s.db, namespace: namespace}
}

func (s *SQLiteStore) Read(value any) error {
	if s.namespace == "" {
		return fmt.Errorf("sqlite value store namespace is empty")
	}
	var encoded string
	err := s.db.QueryRow(`SELECT value_json FROM app_settings WHERE namespace = ?`, s.namespace).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sqlite setting %q: %w", s.namespace, err)
	}
	if err := json.Unmarshal([]byte(encoded), value); err != nil {
		return fmt.Errorf("decode sqlite setting %q: %w", s.namespace, err)
	}
	return nil
}

func (s *SQLiteStore) Write(value any) error {
	if s.namespace == "" {
		return fmt.Errorf("sqlite value store namespace is empty")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode sqlite setting %q: %w", s.namespace, err)
	}
	_, err = s.db.Exec(`
		INSERT INTO app_settings(namespace, value_json, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(namespace) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
	`, s.namespace, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write sqlite setting %q: %w", s.namespace, err)
	}
	return nil
}

func (s *SQLiteStore) Exists() (bool, error) {
	if s.namespace == "" {
		return false, fmt.Errorf("sqlite value store namespace is empty")
	}
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM app_settings WHERE namespace = ?)`, s.namespace).Scan(&exists)
	return exists == 1, err
}

// ErrSMSIdentityMissing 是 InsertSMS 在 SIM 身份为空时返回的分类错误: 调用方
// (modem 路径) 保留模组条目并重试身份获取, 而不是把消息静默归入共享空身份键。
var ErrSMSIdentityMissing = errors.New("sms sim identity is missing")

// InsertSMS 写入一条消息。去重唯一键含 SIM 身份 (direction, iccid, sender,
// recipient, body, received_at), 因此 iccid 必须非空: 身份缺失时拒绝写入,
// 由调用方保留条目并重试。
func (s *SQLiteStore) InsertSMS(record SMSRecord) error {
	if record.Direction != "inbound" && record.Direction != "outbound" {
		return fmt.Errorf("unsupported sms direction %q", record.Direction)
	}
	if strings.TrimSpace(record.ICCID) == "" {
		return ErrSMSIdentityMissing
	}
	if record.Body == "" || record.ReceivedAt.IsZero() {
		return fmt.Errorf("sms body and received_at are required")
	}
	recordedAt := record.RecordedAt
	if recordedAt.IsZero() {
		// Callers that predate the recorded_at attribute keep insertion time.
		recordedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO sms_messages(
			direction, provider_id, sender, recipient, body, received_at, iccid,
			concat_ref, part_number, total_parts, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.Direction, nullableInt(record.ProviderID), record.Sender, record.Recipient,
		record.Body, record.ReceivedAt.UTC().Format(time.RFC3339Nano), record.ICCID, nullableInt(record.ConcatRef),
		nullableInt(record.PartNumber), nullableInt(record.TotalParts), recordedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert sms record: %w", err)
	}
	return nil
}

// SMSListDefaultLimit 是 ListSMS 的默认页大小: 非正 limit 使用该有界默认值,
// 内部列表永远有界, 由应用服务逐页迭代 (design D16)。
const SMSListDefaultLimit = 200

// ListSMS 按方向返回一页消息 (created_at DESC, id DESC)。limit<=0 使用
// defaultSMSListLimit; offset<0 视为 0。调用方 (SMS 应用服务) 内部迭代全部
// 页, 公共 HTTP 契约不变。
func (s *SQLiteStore) ListSMS(direction string, limit, offset int) ([]SMSRecord, error) {
	if limit <= 0 {
		limit = SMSListDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`
		SELECT provider_id, sender, recipient, body, received_at, iccid, concat_ref, part_number, total_parts, created_at
		FROM sms_messages WHERE direction = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?
	`, direction, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list sms records: %w", err)
	}
	defer rows.Close()
	var records []SMSRecord
	for rows.Next() {
		var record SMSRecord
		var providerID, concatRef, partNumber, totalParts sql.NullInt64
		var receivedAt, recordedAt string
		if err := rows.Scan(&providerID, &record.Sender, &record.Recipient, &record.Body, &receivedAt, &record.ICCID, &concatRef, &partNumber, &totalParts, &recordedAt); err != nil {
			return nil, fmt.Errorf("scan sms record: %w", err)
		}
		record.Direction = direction
		record.ProviderID = nullInt(providerID)
		record.ConcatRef = nullInt(concatRef)
		record.PartNumber = nullInt(partNumber)
		record.TotalParts = nullInt(totalParts)
		record.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedAt)
		if err != nil {
			return nil, fmt.Errorf("parse sms received_at: %w", err)
		}
		record.RecordedAt, err = time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil {
			return nil, fmt.Errorf("parse sms created_at: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sms records: %w", err)
	}
	return records, nil
}

func (s *SQLiteStore) InsertCall(record CallRecord) error {
	if record.ID == "" || record.Direction == "" || record.State == "" || record.StartedAt.IsZero() || record.UpdatedAt.IsZero() {
		return fmt.Errorf("call id, direction, state, started_at and updated_at are required")
	}
	_, err := s.db.Exec(`
		INSERT INTO call_records(
			id, call_index, direction, state, number, started_at, updated_at,
			ended_at, connected_at, missed, iccid, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			call_index = excluded.call_index,
			direction = excluded.direction,
			state = excluded.state,
			number = excluded.number,
			started_at = excluded.started_at,
			updated_at = excluded.updated_at,
			ended_at = excluded.ended_at,
			connected_at = excluded.connected_at,
			missed = excluded.missed,
			iccid = excluded.iccid
	`, record.ID, record.Index, record.Direction, record.State, record.Number,
		record.StartedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		formatNullableTime(record.EndedAt), formatNullableTime(record.ConnectedAt), boolInt(record.Missed), record.ICCID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert call record: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListCalls(limit int) ([]CallRecord, error) {
	query := `
		SELECT id, call_index, direction, state, number, started_at, updated_at, ended_at, connected_at, missed, iccid
		FROM call_records ORDER BY started_at DESC, id DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list call records: %w", err)
	}
	defer rows.Close()
	var records []CallRecord
	for rows.Next() {
		var record CallRecord
		var startedAt, updatedAt string
		var endedAt, connectedAt sql.NullString
		var missed int
		if err := rows.Scan(&record.ID, &record.Index, &record.Direction, &record.State, &record.Number, &startedAt, &updatedAt, &endedAt, &connectedAt, &missed, &record.ICCID); err != nil {
			return nil, fmt.Errorf("scan call record: %w", err)
		}
		record.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse call started_at: %w", err)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse call updated_at: %w", err)
		}
		if endedAt.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, endedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse call ended_at: %w", parseErr)
			}
			record.EndedAt = &parsed
		}
		if connectedAt.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, connectedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse call connected_at: %w", parseErr)
			}
			record.ConnectedAt = &parsed
		}
		record.Missed = missed != 0
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate call records: %w", err)
	}
	return records, nil
}

// RecordTrafficSample updates the daily usage total and returns the current
// day's row. The first sample for an ICCID establishes a counter baseline.
func (s *SQLiteStore) RecordTrafficSample(iccid string, sampledAt time.Time, rxCounter, txCounter uint64) (TrafficDailyRecord, error) {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || sampledAt.IsZero() {
		return TrafficDailyRecord{}, fmt.Errorf("iccid and sampled_at are required")
	}
	usageDate := sampledAt.Local().Format("2006-01-02")
	sampledAtValue := sampledAt.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("begin traffic sample: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var previousRX, previousTX uint64
	var previousSampledAt string
	err = tx.QueryRow(`
		SELECT last_rx_counter, last_tx_counter, last_sampled_at
		FROM traffic_daily WHERE iccid = ?
		ORDER BY last_sampled_at DESC LIMIT 1
	`, iccid).Scan(&previousRX, &previousTX, &previousSampledAt)
	if errors.Is(err, sql.ErrNoRows) {
		previousSampledAt = ""
	} else if err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("read traffic baseline: %w", err)
	}
	if previousSampledAt != "" && sampledAtValue <= previousSampledAt {
		return TrafficDailyRecord{}, fmt.Errorf("traffic sample is older than the stored baseline")
	}

	deltaRX, deltaTX := uint64(0), uint64(0)
	if previousSampledAt != "" {
		deltaRX = counterDelta(previousRX, rxCounter)
		deltaTX = counterDelta(previousTX, txCounter)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`
		INSERT INTO traffic_daily(
			iccid, usage_date, rx_bytes, tx_bytes, last_rx_counter,
			last_tx_counter, last_sampled_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(iccid, usage_date) DO UPDATE SET
			rx_bytes = traffic_daily.rx_bytes + excluded.rx_bytes,
			tx_bytes = traffic_daily.tx_bytes + excluded.tx_bytes,
			last_rx_counter = excluded.last_rx_counter,
			last_tx_counter = excluded.last_tx_counter,
			last_sampled_at = excluded.last_sampled_at,
			updated_at = excluded.updated_at
	`, iccid, usageDate, deltaRX, deltaTX, rxCounter, txCounter, sampledAtValue, now)
	if err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("write traffic daily total: %w", err)
	}
	var record TrafficDailyRecord
	var lastSampledAt string
	err = tx.QueryRow(`
		SELECT iccid, usage_date, rx_bytes, tx_bytes, last_rx_counter,
			last_tx_counter, last_sampled_at
		FROM traffic_daily WHERE iccid = ? AND usage_date = ?
	`, iccid, usageDate).Scan(
		&record.ICCID, &record.UsageDate, &record.RXBytes, &record.TXBytes,
		&record.LastRXCounter, &record.LastTXCounter, &lastSampledAt,
	)
	if err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("read traffic daily total: %w", err)
	}
	record.LastSampledAt, err = time.Parse(time.RFC3339Nano, lastSampledAt)
	if err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("parse traffic sampled_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TrafficDailyRecord{}, fmt.Errorf("commit traffic sample: %w", err)
	}
	return record, nil
}

func (s *SQLiteStore) TrafficDaily(iccid, usageDate string) (TrafficDailyRecord, bool, error) {
	var record TrafficDailyRecord
	var lastSampledAt string
	err := s.db.QueryRow(`
		SELECT iccid, usage_date, rx_bytes, tx_bytes, last_rx_counter,
			last_tx_counter, last_sampled_at
		FROM traffic_daily WHERE iccid = ? AND usage_date = ?
	`, strings.TrimSpace(iccid), usageDate).Scan(
		&record.ICCID, &record.UsageDate, &record.RXBytes, &record.TXBytes,
		&record.LastRXCounter, &record.LastTXCounter, &lastSampledAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TrafficDailyRecord{}, false, nil
	}
	if err != nil {
		return TrafficDailyRecord{}, false, fmt.Errorf("read traffic daily total: %w", err)
	}
	record.LastSampledAt, err = time.Parse(time.RFC3339Nano, lastSampledAt)
	if err != nil {
		return TrafficDailyRecord{}, false, fmt.Errorf("parse traffic sampled_at: %w", err)
	}
	return record, true, nil
}

func (s *SQLiteStore) ListTrafficDaily(iccid, fromDate, toDate string) ([]TrafficDailyRecord, error) {
	rows, err := s.db.Query(`
		SELECT iccid, usage_date, rx_bytes, tx_bytes, last_rx_counter,
			last_tx_counter, last_sampled_at
		FROM traffic_daily
		WHERE iccid = ? AND usage_date BETWEEN ? AND ?
		ORDER BY usage_date ASC
	`, strings.TrimSpace(iccid), fromDate, toDate)
	if err != nil {
		return nil, fmt.Errorf("list traffic daily totals: %w", err)
	}
	defer rows.Close()
	var records []TrafficDailyRecord
	for rows.Next() {
		var record TrafficDailyRecord
		var lastSampledAt string
		if err := rows.Scan(
			&record.ICCID, &record.UsageDate, &record.RXBytes, &record.TXBytes,
			&record.LastRXCounter, &record.LastTXCounter, &lastSampledAt,
		); err != nil {
			return nil, fmt.Errorf("scan traffic daily total: %w", err)
		}
		record.LastSampledAt, err = time.Parse(time.RFC3339Nano, lastSampledAt)
		if err != nil {
			return nil, fmt.Errorf("parse traffic daily sampled_at: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traffic daily totals: %w", err)
	}
	return records, nil
}

func counterDelta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	return current
}

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullInt(value sql.NullInt64) int {
	if !value.Valid {
		return 0
	}
	return int(value.Int64)
}

// UpsertSimProfileObserved records a physical SIM or eSIM observation while
// preserving all local metadata.
func (s *SQLiteStore) UpsertSimProfileObserved(record SimProfileRecord) error {
	iccid := strings.TrimSpace(record.ICCID)
	if iccid == "" || len(iccid) > 22 {
		return fmt.Errorf("sim profile iccid is invalid")
	}
	profileType := normalizeSimProfileType(record.ProfileType)
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO sim_profiles(
			iccid, imsi, msisdn, name, local_phone, notes, tags, profile_type,
			first_seen_at, last_seen_at, updated_at
		)
		VALUES(?, ?, ?, ?, '', '', '', ?, ?, ?, ?)
		ON CONFLICT(iccid) DO UPDATE SET
			imsi = CASE WHEN excluded.imsi = '' THEN sim_profiles.imsi ELSE excluded.imsi END,
			msisdn = CASE WHEN excluded.msisdn = '' THEN sim_profiles.msisdn ELSE excluded.msisdn END,
			profile_type = CASE
				WHEN excluded.profile_type = 'unknown' THEN sim_profiles.profile_type
				WHEN sim_profiles.profile_type = 'unknown' THEN excluded.profile_type
				ELSE sim_profiles.profile_type
			END,
			last_seen_at = excluded.last_seen_at,
			updated_at = excluded.updated_at
	`, iccid, strings.TrimSpace(record.IMSI), strings.TrimSpace(record.MSISDN), strings.TrimSpace(record.Name), profileType, now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert sim profile observation: %w", err)
	}
	return nil
}

func normalizeSimProfileType(profileType SimProfileType) SimProfileType {
	switch profileType {
	case SimProfilePhysical, SimProfileESIM:
		return profileType
	default:
		return SimProfileUnknown
	}
}

// InsertSimProfile manually creates one canonical Profile record.
func (s *SQLiteStore) InsertSimProfile(record SimProfileRecord) error {
	iccid := strings.TrimSpace(record.ICCID)
	if iccid == "" || len(iccid) > 22 {
		return fmt.Errorf("sim profile iccid is invalid")
	}
	now := time.Now().UTC()
	firstSeen := record.FirstSeen
	if firstSeen.IsZero() {
		firstSeen = now
	}
	_, err := s.db.Exec(`
		INSERT INTO sim_profiles(
			iccid, imsi, msisdn, name, local_phone, notes, tags, profile_type,
			first_seen_at, last_seen_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, iccid, record.IMSI, record.MSISDN, record.Name, record.LocalPhone, record.Notes,
		record.Tags, normalizeSimProfileType(record.ProfileType),
		firstSeen.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert sim profile: %w", err)
	}
	return nil
}

// UpdateSimProfileMeta updates only user-owned metadata. Empty values clear the
// corresponding field; observed IMSI/MSISDN remain unchanged.
func (s *SQLiteStore) UpdateSimProfileMeta(iccid, name, localPhone, notes, tags string) (bool, error) {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || len(iccid) > 22 {
		return false, fmt.Errorf("sim profile iccid is invalid")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`
		UPDATE sim_profiles SET
			name = ?,
			local_phone = ?,
			notes = ?,
			tags = ?,
			updated_at = ?
		WHERE iccid = ?
	`, strings.TrimSpace(name), strings.TrimSpace(localPhone), strings.TrimSpace(notes), strings.TrimSpace(tags), now, iccid)
	if err != nil {
		return false, fmt.Errorf("update sim profile metadata: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update sim profile rows: %w", err)
	}
	return affected > 0, nil
}

func (s *SQLiteStore) DeleteSimProfile(iccid string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM sim_profiles WHERE iccid = ?`, strings.TrimSpace(iccid))
	if err != nil {
		return false, fmt.Errorf("delete sim profile: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete sim profile rows: %w", err)
	}
	return affected > 0, nil
}

func (s *SQLiteStore) ListSimProfiles() ([]SimProfileRecord, error) {
	rows, err := s.db.Query(`
		SELECT iccid, imsi, msisdn, name, local_phone, notes, tags, profile_type,
			first_seen_at, last_seen_at, updated_at
		FROM sim_profiles ORDER BY last_seen_at DESC, iccid ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list sim profiles: %w", err)
	}
	defer rows.Close()
	var records []SimProfileRecord
	for rows.Next() {
		var record SimProfileRecord
		var firstSeen, lastSeen, updatedAt string
		if err := rows.Scan(&record.ICCID, &record.IMSI, &record.MSISDN, &record.Name,
			&record.LocalPhone, &record.Notes, &record.Tags, &record.ProfileType,
			&firstSeen, &lastSeen, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan sim profile: %w", err)
		}
		record.FirstSeen, err = time.Parse(time.RFC3339Nano, firstSeen)
		if err != nil {
			return nil, fmt.Errorf("parse sim profile first_seen_at: %w", err)
		}
		record.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
		if err != nil {
			return nil, fmt.Errorf("parse sim profile last_seen_at: %w", err)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse sim profile updated_at: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sim profiles: %w", err)
	}
	return records, nil
}

// UpsertNotificationHistory 写入一条 eUICC 通知历史。去重键
// (sequence_number, iccid, event)；已存在时更新状态/地址/更新时间，
// 首次观察时间保持第一次写入不变。卡片列表同步写入 pending 时，不覆盖
// 用户操作已经记录的终态。
func (s *SQLiteStore) UpsertNotificationHistory(record NotificationHistoryRecord) error {
	if record.SequenceNumber <= 0 || strings.TrimSpace(record.Event) == "" {
		return fmt.Errorf("notification sequence and event are required")
	}
	now := time.Now().UTC()
	observedAt := record.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	state := record.State
	if strings.TrimSpace(string(state)) == "" {
		state = NotificationStatePending
	}
	_, err := s.db.Exec(`
		INSERT INTO esim_notification_history(
			sequence_number, event, iccid, address, aid, state, observed_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sequence_number, iccid, event) DO UPDATE SET
			address = excluded.address,
			aid = excluded.aid,
			state = CASE
				WHEN excluded.state = 'pending' AND esim_notification_history.state <> 'pending'
				THEN esim_notification_history.state
				ELSE excluded.state
			END,
			updated_at = excluded.updated_at
	`, record.SequenceNumber, record.Event, record.ICCID, record.Address, record.AID,
		string(state), observedAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert notification history: %w", err)
	}
	return nil
}

// UpdateNotificationHistoryState records an explicit user-driven outcome for
// the most recently observed notification with the supplied sequence number.
// Sequence numbers can be reused across cards, while the action endpoint only
// carries the current sequence, so older matching history must remain intact.
func (s *SQLiteStore) UpdateNotificationHistoryState(sequenceNumber int64, state NotificationHistoryState) error {
	if sequenceNumber <= 0 {
		return fmt.Errorf("notification sequence is required")
	}
	if strings.TrimSpace(string(state)) == "" {
		return fmt.Errorf("notification state is required")
	}
	if _, err := s.db.Exec(`
		UPDATE esim_notification_history SET state = ?, updated_at = ?
		WHERE rowid = (
			SELECT rowid FROM esim_notification_history
			WHERE sequence_number = ?
			ORDER BY updated_at DESC, rowid DESC LIMIT 1
		)
	`, string(state), time.Now().UTC().Format(time.RFC3339Nano), sequenceNumber); err != nil {
		return fmt.Errorf("update notification history state: %w", err)
	}
	return nil
}

// MarkNotificationHistoryAbsent 把仍处于 pending 状态、但不在当前卡片待处理
// 快照中的记录标记为 processed——说明通知已被自动清理或卡片自行处置。
// current 是本次快照中仍在卡片上的记录键集合。
func (s *SQLiteStore) MarkNotificationHistoryAbsent(current []NotificationHistoryRecord) error {
	rows, err := s.db.Query(`
		SELECT sequence_number, iccid, event FROM esim_notification_history WHERE state = ?
	`, NotificationStatePending)
	if err != nil {
		return fmt.Errorf("read pending notification history: %w", err)
	}
	// 单连接池（MaxOpenConns=1）下，rows 未关闭前不能发起新的 Exec，
	// 否则写语句会永远等不到连接。先读完并关闭 rows，再执行更新。
	var pending [][3]string
	for rows.Next() {
		var sequenceNumber int64
		var iccid, event string
		if err := rows.Scan(&sequenceNumber, &iccid, &event); err != nil {
			rows.Close()
			return fmt.Errorf("scan pending notification history: %w", err)
		}
		pending = append(pending, [3]string{fmt.Sprintf("%d", sequenceNumber), iccid, event})
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return fmt.Errorf("iterate pending notification history: %w", rowsErr)
	}
	present := make(map[string]bool, len(current))
	for _, record := range current {
		present[notificationHistoryKey(record.SequenceNumber, record.ICCID, record.Event)] = true
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for _, key := range pending {
		if present[notificationHistoryKeyRaw(key[0], key[1], key[2])] {
			continue
		}
		if _, err := s.db.Exec(`
			UPDATE esim_notification_history SET state = ?, updated_at = ?
			WHERE state = ? AND sequence_number = ? AND iccid = ? AND event = ?
		`, NotificationStateProcessed, updatedAt,
			NotificationStatePending, key[0], key[1], key[2]); err != nil {
			return fmt.Errorf("mark notification history absent: %w", err)
		}
	}
	return nil
}

func notificationHistoryKey(sequenceNumber int64, iccid, event string) string {
	return fmt.Sprintf("%d|%s|%s", sequenceNumber, iccid, event)
}

func notificationHistoryKeyRaw(sequenceNumber, iccid, event string) string {
	return sequenceNumber + "|" + iccid + "|" + event
}

// NotificationHistoryDefaultLimit 是历史列表的默认页大小。
const NotificationHistoryDefaultLimit = 200

// ListNotificationHistory 按处置时间倒序返回通知历史。limit<=0 使用默认值。
func (s *SQLiteStore) ListNotificationHistory(limit int) ([]NotificationHistoryRecord, error) {
	if limit <= 0 {
		limit = NotificationHistoryDefaultLimit
	}
	rows, err := s.db.Query(`
		SELECT sequence_number, event, iccid, address, aid, state, observed_at, updated_at
		FROM esim_notification_history ORDER BY updated_at DESC, sequence_number DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list notification history: %w", err)
	}
	defer rows.Close()
	var records []NotificationHistoryRecord
	for rows.Next() {
		var record NotificationHistoryRecord
		var state, observedAt, updatedAt string
		if err := rows.Scan(&record.SequenceNumber, &record.Event, &record.ICCID, &record.Address,
			&record.AID, &state, &observedAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan notification history: %w", err)
		}
		record.State = NotificationHistoryState(state)
		record.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt)
		if err != nil {
			return nil, fmt.Errorf("parse notification observed_at: %w", err)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse notification updated_at: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification history: %w", err)
	}
	return records, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
