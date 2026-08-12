## Why

The application starts from platform discovery and does not load the legacy YAML device configuration. The `internal/config` package now preserves an obsolete configuration model, file mutators, and unused helpers while still coupling the modem manager to that model.

## What Changes

- **BREAKING** Remove the `internal/config` package and its YAML device configuration API.
- Replace `config.DeviceConfig` with a minimal modem-owned runtime configuration type.
- Migrate backend and modem tests to the new runtime type.
- Remove the YAML dependency and any unused legacy configuration helpers.
- Document that device startup uses discovery and runtime state, not `config.yaml`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `modem-backends`: Backend construction uses runtime-discovered device data and does not depend on the removed legacy configuration package.

## Impact

- Affected Go code: `internal/backend/at_factory.go`, `internal/modem/manager.go`, and related tests.
- Removed Go code: `internal/config/`.
- Dependency metadata: `go.mod` and `go.sum` after `go mod tidy`.
- Documentation: `SOURCE_STRUCTURE.md` and the backend code map if it references the legacy package.
- No HTTP, WebSocket, capability, storage schema, or device lifecycle contract changes are intended.
