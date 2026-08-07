package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func createV5Database(t *testing.T, path, profileNotes string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version, applied_at) VALUES
			(1, '2026-08-01T00:00:00Z'), (2, '2026-08-02T00:00:00Z'),
			(3, '2026-08-03T00:00:00Z'), (4, '2026-08-04T00:00:00Z'),
			(5, '2026-08-05T00:00:00Z');
		CREATE TABLE app_settings (
			namespace TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE TABLE sim_cards (
			iccid TEXT PRIMARY KEY, imsi TEXT NOT NULL DEFAULT '', msisdn TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
			first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE INDEX idx_sim_cards_last_seen ON sim_cards(last_seen_at DESC);
		INSERT INTO sim_cards(iccid, imsi, msisdn, name, notes, first_seen_at, last_seen_at, updated_at)
		VALUES('8901000000000000001', '460001234', '+8613900000000', 'Canonical', 'existing note',
			'2026-08-01T00:00:00Z', '2026-08-05T00:00:00Z', '2026-08-05T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	if profileNotes != "" {
		if _, err := db.Exec(`INSERT INTO app_settings(namespace, value_json, updated_at) VALUES('profile_notes', ?, '2026-08-05T00:00:00Z')`, profileNotes); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteMigratesV5SIMProfilesAndNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.sqlite3")
	createV5Database(t, path, `{
		"8901000000000000001":{"label":"Legacy Label","phone":"+8613800000001","tags":"travel"},
		"8901000000000000002":{"label":"Second","phone":"+8613800000002","tags":"backup"}
	}`)

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	profiles, err := store.ListSimProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
	byICCID := map[string]SimProfileRecord{}
	for _, profile := range profiles {
		byICCID[profile.ICCID] = profile
	}
	first := byICCID["8901000000000000001"]
	if first.Name != "Canonical" || first.LocalPhone != "+8613800000001" || first.Tags != "travel" || first.ProfileType != SimProfileESIM {
		t.Fatalf("merged first profile = %+v", first)
	}
	if !strings.Contains(first.Notes, "existing note") || !strings.Contains(first.Notes, "Migrated eSIM label: Legacy Label") {
		t.Fatalf("conflicting label not preserved: %q", first.Notes)
	}
	second := byICCID["8901000000000000002"]
	if second.Name != "Second" || second.LocalPhone != "+8613800000002" || second.Tags != "backup" || second.ProfileType != SimProfileESIM {
		t.Fatalf("migrated second profile = %+v", second)
	}

	var legacyTables, legacyNotes int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sim_cards'`).Scan(&legacyTables); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM app_settings WHERE namespace='profile_notes'`).Scan(&legacyNotes); err != nil {
		t.Fatal(err)
	}
	if legacyTables != 0 || legacyNotes != 0 {
		t.Fatalf("legacy state remains: tables=%d notes=%d", legacyTables, legacyNotes)
	}
}

func TestSQLiteV6MigrationRollsBackOnInvalidNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5-invalid.sqlite3")
	createV5Database(t, path, `{invalid`)

	if store, err := OpenSQLite(path); err == nil {
		_ = store.Close()
		t.Fatal("OpenSQLite succeeded with invalid legacy notes")
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var version, legacyRows, legacyNotes int
	if err := check.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := check.QueryRow(`SELECT COUNT(*) FROM sim_cards`).Scan(&legacyRows); err != nil {
		t.Fatal(err)
	}
	if err := check.QueryRow(`SELECT COUNT(*) FROM app_settings WHERE namespace='profile_notes'`).Scan(&legacyNotes); err != nil {
		t.Fatal(err)
	}
	if version != 5 || legacyRows != 1 || legacyNotes != 1 {
		t.Fatalf("rollback failed: version=%d rows=%d notes=%d", version, legacyRows, legacyNotes)
	}
}

func TestSQLiteFreshDatabaseUsesOnlySIMProfiles(t *testing.T) {
	store := openTestStore(t)
	var profiles, legacy int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sim_profiles'`).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sim_cards'`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if profiles != 1 || legacy != 0 {
		t.Fatalf("tables: sim_profiles=%d sim_cards=%d", profiles, legacy)
	}
}
