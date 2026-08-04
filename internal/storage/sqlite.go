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
	ID        string
	Index     int
	Direction string
	State     string
	Number    string
	StartedAt time.Time
	UpdatedAt time.Time
	EndedAt   *time.Time
	Missed    bool
	ICCID     string
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
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record sqlite schema migration: %w", err)
		}
	}
	return nil
}

// migrationVersion is the newest schema version this binary understands.
const migrationVersion = 2

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

func (s *SQLiteStore) InsertSMS(record SMSRecord) error {
	if record.Direction != "inbound" && record.Direction != "outbound" {
		return fmt.Errorf("unsupported sms direction %q", record.Direction)
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

func (s *SQLiteStore) ListSMS(direction string) ([]SMSRecord, error) {
	rows, err := s.db.Query(`
		SELECT provider_id, sender, recipient, body, received_at, iccid, concat_ref, part_number, total_parts, created_at
		FROM sms_messages WHERE direction = ? ORDER BY created_at DESC, id DESC
	`, direction)
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
			ended_at, missed, iccid, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			call_index = excluded.call_index,
			direction = excluded.direction,
			state = excluded.state,
			number = excluded.number,
			started_at = excluded.started_at,
			updated_at = excluded.updated_at,
			ended_at = excluded.ended_at,
			missed = excluded.missed,
			iccid = excluded.iccid
	`, record.ID, record.Index, record.Direction, record.State, record.Number,
		record.StartedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		formatNullableTime(record.EndedAt), boolInt(record.Missed), record.ICCID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert call record: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListCalls(limit int) ([]CallRecord, error) {
	query := `
		SELECT id, call_index, direction, state, number, started_at, updated_at, ended_at, missed, iccid
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
		var endedAt sql.NullString
		var missed int
		if err := rows.Scan(&record.ID, &record.Index, &record.Direction, &record.State, &record.Number, &startedAt, &updatedAt, &endedAt, &missed, &record.ICCID); err != nil {
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

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
