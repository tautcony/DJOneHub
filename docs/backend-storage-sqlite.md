# Backend Storage Design

## Overview

DJOneHub stores local application state in one SQLite database. The database is
created under the user configuration directory:

```text
<UserConfigDir>/DJOneHub/djonehub.sqlite3
```

On macOS, `UserConfigDir` is normally `~/Library/Application Support`. The
database is local to the desktop installation and is not a server or a shared
network database.

SQLite replaces the previous collection of independent JSON files. JSON is
still read during startup only to migrate existing installations. Legacy files
are not deleted so that the migration is recoverable and idempotent.

## Connection And Migration

`internal/storage/sqlite.go` opens SQLite with the `modernc.org/sqlite` driver
and applies these settings:

- `journal_mode=WAL` permits readers while a write is in progress.
- `foreign_keys=ON` enables referential-integrity checks for future relations.
- `busy_timeout=5000` gives short-lived concurrent operations time to finish.
- The connection pool is limited to one open connection because this is a
  single-user local store and it keeps SQLite locking behavior predictable.

Schema creation is idempotent. Version `1` is recorded in `schema_migrations`;
future incompatible changes must add a new migration version rather than
editing an existing table in place.

Startup migration order:

1. Open the database and create the schema.
2. Import `profile-notes.json` into the `profile_notes` settings namespace when
   that namespace does not exist.
3. Import `notification-preferences.json` into the
   `notification_preferences` namespace when absent.
4. Import `sms-sent-history.json` as outbound rows. Inserts are deduplicated by
   the SMS uniqueness constraint, so repeating startup is safe.
5. Start application services using SQLite-backed stores.

## Schema

### `schema_migrations`

| Column | Type | Rules | Description |
| --- | --- | --- | --- |
| `version` | `INTEGER` | Primary key | Applied schema version. |
| `applied_at` | `TEXT` | Not null | UTC RFC3339 timestamp. |

### `app_settings`

This table stores small typed settings documents as JSON. A namespace is the
ownership boundary; services do not read or write another service's namespace.

| Column | Type | Rules | Description |
| --- | --- | --- | --- |
| `namespace` | `TEXT` | Primary key | Stable settings namespace. |
| `value_json` | `TEXT` | Not null | JSON representation of the value. |
| `updated_at` | `TEXT` | Not null | UTC RFC3339 timestamp of the last write. |

Current namespaces:

- `profile_notes`: device profile notes keyed by profile/device identifier.
- `notification_preferences`: notification mode, sound, and related policy
  settings.

Writes use an SQLite upsert, so a settings update is atomic for the complete
document.

### `sms_messages`

SMS messages use rows instead of one growing JSON array. Both inbound and
outbound directions are supported by the schema.

| Column | Type | Rules | Description |
| --- | --- | --- | --- |
| `id` | `INTEGER` | Primary key autoincrement | Local row identifier. |
| `direction` | `TEXT` | `inbound` or `outbound` | Message direction. |
| `provider_id` | `INTEGER` | Nullable | Modem/provider message index when available. |
| `sender` | `TEXT` | Not null, default empty | Sender phone number. |
| `recipient` | `TEXT` | Not null, default empty | Recipient phone number. |
| `body` | `TEXT` | Not null | Message body. |
| `received_at` | `TEXT` | Not null | Message timestamp in UTC RFC3339 format. |
| `concat_ref` | `INTEGER` | Nullable | Concatenated SMS reference. |
| `part_number` | `INTEGER` | Nullable | Concatenated SMS part number. |
| `total_parts` | `INTEGER` | Nullable | Total concatenated SMS parts. |
| `created_at` | `TEXT` | Not null | Local insertion timestamp in UTC. |

The uniqueness constraint on `(direction, sender, recipient, body,
received_at)` makes migration and retries idempotent. The indexes are:

- `idx_sms_messages_received_at` for newest-message queries.
- `idx_sms_messages_peer_time` for conversation/peer history queries.

The application currently persists outbound messages when a send operation
successfully completes. The modem remains the source of truth for inbound
messages: `SMSService.Refresh` reads the modem inbox, reassembles multipart
messages, merges them into the runtime cache, and publishes events. Inbound
rows can be enabled later without changing the schema or settings model.

## Retention And Cleanup

The SMS service keeps the newest 500 outbound messages and the newest 500
runtime messages for the current process. SQLite does not silently delete rows;
the database is therefore a complete local history of successfully persisted
outbound sends. A future explicit retention job may delete old rows in a
transaction after a user-facing policy is defined.

`Clear` currently clears the modem inbox only. It does not delete the local
SQLite history, which prevents a device operation from unexpectedly destroying
the sent-message audit trail.

## Concurrency And Error Handling

Settings writes and SMS inserts are single SQL statements and are atomic. The
store uses one connection and a busy timeout, while service-level mutexes
protect in-memory message caches. A modem send is considered successful once
the provider accepts it; a subsequent local-history write error is not returned
as a send failure because reporting failure could cause a duplicate SMS retry.
Such errors should be surfaced through application logging/diagnostics when a
production logging policy is added.

## Backup And Recovery

Before copying a live database, stop DJOneHub or use SQLite's online backup
mechanism. Copy the database together with its `-wal` and `-shm` files when the
process is running. The old JSON files are retained after migration as a
fallback source. Restoring a database requires preserving file permissions
(`0700` for the directory and `0600` for the database where supported).

