## 1. Dead-Code and Dependency Cleanup (D1-D2)

- [ ] 1.1 Grep the root product for callers of `config.Load`, `config.GetConfig`, `config.UpdateNotificationInFile`, and the Telegram/Feishu/QQ/Webhook/Bark/Email/Pushplus notification-channel config types; then delete the legacy surface and the shared-pointer accessors in `internal/config/config.go`, `manager.go`, and `persist.go`
- [ ] 1.2 Remove `github.com/spf13/viper` from the root `go.mod`; run `go mod tidy` and confirm viper no longer appears in `go.mod`/`go.sum`; keep `file-rotatelogs` if `pkg/logger` rotation still requires it
- [ ] 1.3 Create the config/state directory with mode 0700 where it is first created (2.6); verify the directory and its persisted files are not world-readable
- [ ] 1.4 Delete the `readerIMSIRegistry` and `LookupIMSIByReader` from `pkg/logger/logger.go:33-57,138-146` and the branches that consult it, after confirming zero writers
- [ ] 1.5 Delete the legacy `AcquireSession`/`AcquireOneShot` interfaces and their compatibility fields from `internal/apduarbiter/arbiter.go:276-280` after confirming zero production callers
- [ ] 1.6 Confirm the remaining root `go.mod` replace directives (all `third_party/` fork patches; the byte-identical `multierr`/`pkg-errors` replaces were already pruned before this change) are genuine patches, and add a short note recording what each remaining fork patches so they cannot drift silently
- [ ] 1.7 Delete the unreferenced `web/src/components/ErrorState.vue` and the unused `webURL` parse in `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift` (3.9 L6)

## 2. Build Hygiene (D4-D5)

- [ ] 2.1 Rework `scripts/build-macos.sh`: replace the `BUILD_ROOT="${TMPDIR:-/tmp}"` + `rm -rf` with `mktemp -d`, validate `VERSION` against a strict pattern before PlistBuddy/`PACKAGE_NAME` use (rejecting `/`), and require `VERSION` as an argument instead of the hardcoded `v0.1.5-preview` fallback
- [ ] 2.2 Add checksum verification to the `internal/esim/pki` `go:generate` step: pin SHA-256 for `ci.json` and `accredited.json`, download to `.tmp`, verify, atomically rename, abort on mismatch, and document how to refresh the checksums deliberately

## 3. Cross-Path Consistency: EF_IMSI and COPS (D8-D9)

- [ ] 3.1 Verify the EF_IMSI byte-to-IMSI mapping on a real device; on confirmation of the standard layout, delete the `imsiStr[1:]` truncation in `internal/modem/commands.go:720-723`
- [ ] 3.2 Add a unit test pinning the standard-layout EF_IMSI vector (e.g. `460009300011111` decodes with MCC 460, not 600) and a cross-path test asserting the AT path and `internal/backend/mbim_backend_simfiles.go:125` produce identical IMSI for the same EF_IMSI content
- [ ] 3.3 Make `QueryOperator` issue only `AT+COPS?` and parse the modem-reported format; remove every query-time `AT+COPS=3,2` fallback. Return a classified parse error when the format is unsupported, and verify repeated polls leave the user's format selection unchanged

## 4. eSIM Arbiter Coverage and AT APDU Unification (D10-D12)

- [ ] 4.1 Thread the device-level `APDUArbiter` (the same instance the modem manager uses) into both pure-AT eSIM construction paths: darwin (`internal/platform/darwin/adapter.go` and app assembly) and Linux/Windows (`serialESIMPortBuilder` in `internal/app/app.go`), so `NewATPort` (`internal/esim/at_port.go`) passes it via `ManagerOptions`; add a test that every `esimPort` path enforces the SIM-switch barrier and APDU-idle waits instead of no-ops. `at_port.go` also gains request-context threading in `fix-reliability-and-lifecycle` task 3.6 — disjoint edits, do not touch each other's code
- [ ] 4.2 Merge `ModemChannel` (`internal/esim/channel.go`) and `ATSmartCardChannel` (`internal/esim/at_channel.go`) into one shared `driver.SmartCardChannel` implementation parameterized by the AT command executor, keeping the `manager.go:495` and `at_port.go:31` wiring; add a table test exercising both transports
- [ ] 4.3 Apply uniform guards in the merged channel: reject `Transmit` on channel zero (3.1 L8) and promote the per-APDU `regexp.MustCompile` to package-level vars (3.1 L4)
- [ ] 4.4 Keep MBIM APDU transport scope explicitly exclusive (`mbim_apdu_transport.go:84-102`) because independent per-channel safety is not established; document the divergence against QMI's per-channel model (`arbiter.go:902-910`) at both sites and keep arbiter tests covering both transports; do not modify the lease/force-release semantics owned by `fix-security-and-data-loss` D5 (disjoint line ranges in arbiter.go)

## 5. Unified Redaction Strategy (D7)

- [ ] 5.1 Replace the CJK-heuristic `publicText` (`internal/api/http/server.go:1225-1255`) with the explicit event-family allowlist and typed projections in `design.md`, scoped to the public event stream (WS events + REST status/snapshot); keep the existing nested `domain.Snapshot`/`device.Status` success-payload shapes, keep device identity (IMEI/ICCID/IMSI/EID) present in status/snapshot payloads (web Overview card renders it client-side masked), and replace only error/reason text with fallback text; leave REST data endpoints (SMS list, call history) outside the sanitizer; add matrix tests for typed and raw/unknown payloads with no unknown-field passthrough, plus a test asserting identity values survive status/snapshot sanitization
- [ ] 5.2 Mask IMEI/ICCID at Info level (full values at Debug) in the modem logging paths (`internal/modem/manager.go:787` and related), and gate SMS content, USSD text, and dialed/incoming numbers behind an explicit logging switch that is off by default (`manager.go:286,289,1422,2043,2060`, `urc_format.go:105-113,192,205`)
- [ ] 5.3 Omit `matchingID` from eSIM download logs (`internal/esim/manager.go:3115-3119`), logging at most a digest
- [ ] 5.4 Add the presence-aware `sender_only` notification preference to `macos/DJOneHubNotifier` (`NativeNotificationService.swift:177-186`, `PanelContent.swift:35-43`, `NotifierView.swift:97-127`): when enabled, the notification request carries no message body; default an absent field to `true` while respecting an explicitly persisted `false`

## 6. App.vue Split and Frontend Hygiene (D6)

- [ ] 6.1 Replace `ViewContext = Record<string, any>` (`web/src/views/context.ts:6`) with a typed interface and migrate routed views to typed members before splitting state
- [ ] 6.2 Move view state out of `App.vue` into per-domain Pinia stores (SMS, eSIM, network, VoWiFi, operations), one domain at a time, keeping `npm run build` and type-check green after each domain
- [ ] 6.3 Bound the operations map in `web/src/stores/device.ts`: remove entries on terminal events or TTL (3.8 L1); replace the fixed 2.5s reconnect with exponential backoff + jitter and a bounded maximum
- [ ] 6.4 Register Ant Design components on demand (`web/src/main.ts`) instead of `app.use(Antd)` (3.8 L2)
- [ ] 6.5 i18n hygiene: move `runVowifi` to a dedicated vowifi namespace (3.8 L3); sync `document.documentElement.lang` with the active locale; render operation/call status through the i18n catalog with a fallback instead of raw keys (3.8 L4)
- [ ] 6.6 Render SMS session lists lazily above a threshold instead of mounting all rows (3.8 L5)

## 7. Runtime, API, and Storage Consistency (D13-D17)

- [ ] 7.1 Declare the single-device constraint in the discovery contract and change platform discovery so only the candidate the runtime consumes is probed (`linux/adapter.go:63-65`, `runtime/runtime.go:120`); do not change probe budgets, cooldowns, or fallbacks (3.5 M6)
- [ ] 7.2 Serialize HTTP rescans with the polling-loop scan through the same lifecycle lock (`runtime/runtime.go:93-95,111-206`) so a concurrent scan cannot re-install a closed backend (3.6 L4)
- [ ] 7.3 Classify `SaveNote` validation failures (missing/over-long iccid) and cancelled operations as explicit `derrors` errors in the services and map them to explicit structured API codes/statuses instead of the generic 500 bucket (`server.go:475-509,421-440,1258-1277`, `extras/service.go:369-371`) (3.6 L1)
- [ ] 7.4 Replace the old SQLite SMS table-level unique constraint transactionally in migration v3 with a uniqueness key containing a non-empty SIM identity; copy existing rows/IDs and recreate indexes before recording the migration. Test a real v2 schema upgrade, rollback on injected migration failure, preservation of IDs/data, cross-SIM duplicates, and rejection/retry when identity is empty. Add bounded internal `LIMIT`/`OFFSET` pagination with a default page to `ListSMS(direction, limit, offset)` (`storage/sqlite.go:118-132,306-342`); the SMS application service iterates all pages internally so the existing refresh response stays complete — keep public HTTP and wire contracts unchanged, no caller-visible truncation (3.6 L2); the SMS delivery pipeline is owned by `fix-security-and-data-loss` D3, coordinate the caller update there
- [ ] 7.5 Publish traffic events only when sampled values change (`network/service.go:82-93,113`) and serve firmware status from a short-TTL cache instead of the full AT + ADB probe sequence on every read (`firmware/service.go:182-261`) (3.6 L3)

## 8. Native UI Hygiene (D18)

- [ ] 8.1 Cache the `ISO8601DateFormatter` instances and avoid re-encoding parsed event JSON in `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/BridgeModels.swift:165-215` (3.9 L1)
- [ ] 8.2 Replace the bare `MainActor.assumeIsolated` in `NativeUIHost.swift:23` with an explicit thread check and readable error, and document the Go LockOSThread contract in `bridge.h` (3.9 L2)
- [ ] 8.3 Set `.timeSensitive` interruption level on incoming call notifications (`NativeNotificationService.swift:147-161`) (3.9 L4)
- [ ] 8.4 Implement `applicationSupportsSecureRestorableState` returning `true` (`NativeUIHost.swift`) (3.9 L5)

## 9. Verification

- [ ] 9.1 Run `go build ./...`, `go vet ./...`, and `go mod tidy` for the root product; confirm viper is gone from the root module and the legacy config/IMSI/arbiter symbols have no root-product references
- [ ] 9.2 Run `go test -race` over `internal/config`, `pkg/logger`, `internal/apduarbiter`, `internal/modem`, `internal/esim`, `internal/runtime`, `internal/api/http`, `internal/application`, `internal/backend`, and `internal/storage`, including redaction matrix tests for every allowlisted event family and raw/unknown payloads, and fix any reported races
- [ ] 9.3 Run `npm run build` and `npx vue-tsc --noEmit` (or the project's type-check command) after the App.vue/store/i18n work
- [ ] 9.4 Verify the PKI `go:generate` fails cleanly on a tampered download and regenerates identically on a clean run
- [ ] 9.5 Smoke-test on hardware: EF_IMSI on AT and MBIM paths returns identical full IMSI; repeated operator polling leaves format selection unchanged and returns an explicit error for unsupported formats; pure-AT eSIM switch waits for APDU idle; Info-level logs contain no IMEI/ICCID/message content/matchingID
- [ ] 9.6 Smoke-test the macOS app: sender-only preference hides SMS bodies in notifications; incoming calls break Focus banners; a bridge event on the wrong thread surfaces the readable threading error
