## 1. Contracts And State

- [x] 1.1 Add bounded `EDLObservation`, Sahara state, session, lease, and structured conflict models.
- [x] 1.2 Extend transport and platform capability contracts for live Sahara observation.
- [x] 1.3 Update device-control, API, event, runtime, platform, and UI delta specs with exact schemas and error details.

## 2. EDL Session Runtime

- [x] 2.1 Implement a runtime-owned single-device EDL session manager keyed by physical location.
- [x] 2.2 Implement acquire, renew, release, expiry, and conflict behavior with race tests.
- [x] 2.3 Preserve session identity across EDL re-enumeration and enter recovery-required on ambiguity or timeout.

## 3. Live Sahara Observation

- [x] 3.1 Implement bounded Sahara handshake and state parsing for the macOS EDL adapter.
- [x] 3.2 Expose masked serial, HWID, PK hash, SBL version, and source metadata without mapping them to AT firmware revision.
- [x] 3.3 Add protocol, timeout, disconnect, malformed-response, and unsupported-platform tests.

## 4. Backup And Reset Semantics

- [x] 4.1 Remove automatic reset and reconnect from successful NAND backup.
- [x] 4.2 Keep explicit reset and same-location reconnect as a separate operation.
- [x] 4.3 Define bounded cancellation cleanup and recovery-required terminal details.
- [x] 4.4 Update capability reasons, operation progress phases, and focused service tests.

## 5. API, Events, And UI

- [x] 5.1 Add lease endpoints and require lease tokens for mutating device-control actions.
- [x] 5.2 Include EDL session state in status and initial WebSocket snapshots.
- [x] 5.3 Render live EDL facts, Sahara state, lease conflicts, and post-backup EDL state.
- [x] 5.4 Add HTTP, event, frontend type, lint, and build coverage.

## 6. Verification And Documentation

- [x] 6.1 Run focused Go tests and `go test -race ./...` for session and lifecycle code.
- [x] 6.2 Run frontend typecheck, lint, and build.
- [x] 6.3 Update `docs/code-map/` and user-facing device-control documentation.
