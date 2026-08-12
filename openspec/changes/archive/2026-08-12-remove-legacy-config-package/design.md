## Context

`cmd/djonehub` calls `app.New`, which creates a platform discovery adapter. The AT factory receives a discovered `device.Candidate` and constructs a configuration value only to initialize `modem.Manager`. No production code loads or reads the legacy YAML configuration.

## Decision

Define a small `modem.Config` type in `internal/modem` with only fields read by the manager: `ID`, `ATPort`, `ManagePort`, `ControlDevice`, `DeviceBackend`, and `ATTimeoutWatchdogThreshold`. Change the manager constructors and the AT factory mapping to use this type.

Delete `internal/config` after all production and test imports are migrated. This removes the YAML persistence functions, IMEI comparison helpers, unused transport normalizers, stale serialization tags, and their tests. Run `go mod tidy` to remove dependencies that become unreachable.

## Compatibility and Safety

The new type preserves the existing defaults and decision logic. `atManagerConfig` continues to derive values from the discovered candidate. No file is read, written, or migrated. Existing SQLite-backed application settings remain owned by `internal/storage`.

The old model contains fields for transport selection, IP family, operator selection, and eSIM switching, but no current production call path consumes them. This change does not add controls for those fields. If a future requirement makes a runtime value user-editable, it must use an explicit SQLite namespace, HTTP contract, and Vue settings control. It must not restore a YAML compatibility layer. The AT watchdog threshold keeps its existing default because the current product has no supported user-facing override.

## Verification

Run focused modem/backend tests, then `go test ./...`, `go vet ./...`, and the frontend checks. Verify that no Go source imports `internal/config`, no `internal/config` directory remains, and YAML/mapstructure dependencies are not retained solely for the removed package.
