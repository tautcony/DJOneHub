## Context

The full-repository code review (docs/code-review-report.md, baseline main @ 965b624) batch 3 covers architectural cleanup (items 11-14). The two sibling changes (`fix-security-and-data-loss`, `fix-reliability-and-lifecycle`) are planned concurrently and already own the temporary loopback boundary, SMS pipeline repair, AT response correctness, eSIM synchronization, event/notification reliability, shutdown ordering, VoWiFi lifecycle convergence, WebSocket migration, and `logger.Setup` wiring. This change must not duplicate those; it targets what they explicitly leave behind: dead code and dependency debt in the root product (2.6, 3.6 M5, 3.10 M4/L6), the `App.vue` god component and frontend hygiene (3.8 M4, L1-L5), the ad-hoc sensitive-data redaction strategy (2.7, 3.1 L3, 3.2 M4, 3.6 L4), and cross-path inconsistencies (3.2 H1/M10, 3.1 M5/L4/L8 + channel merge, 3.5 M6, 3.6 L1-L4, 3.9 L1-L6).

Verified at baseline: root `go.mod` still requires `github.com/spf13/viper`; `internal/config` carries `GetConfig`/`UpdateNotificationInFile` and the legacy notification-channel config types; `pkg/logger/logger.go:33-57,138-146` holds the never-written `readerIMSIRegistry`; `internal/apduarbiter/arbiter.go:276-280` still exposes the `AcquireSession`/`AcquireOneShot` legacy interfaces (zero root-product callers); `internal/esim/channel.go` (`ModemChannel`) and `internal/esim/at_channel.go` (`ATSmartCardChannel`) are two near-identical AT APDU channel wrappers; `NewATPort` (`internal/esim/at_port.go:24-33`) never passes `APDUArbiter` (the `ManagerOptions.APDUArbiter` plumbing already exists at `manager.go:479-480`); `internal/modem/commands.go:720-723` truncates the first EF_IMSI digit on the AT path only (MBIM at `internal/backend/mbim_backend_simfiles.go:125` does not); `commands.go:62` (`QueryOperator`) issues `AT+COPS=3,2` on every pure-query poll; `web/src/App.vue` is 1396 lines with `ViewContext = Record<string, any>` (`web/src/views/context.ts:6`); the events endpoint sanitizes via the CJK-heuristic `publicText` (`internal/api/http/server.go:1225-1255`).

## Goals / Non-Goals

**Goals:**

- Remove dead code and the dependencies it drags in from the root product: the legacy viper config surface, the IMSI registry, and legacy arbiter session interfaces; document the patch purpose of every remaining root module replace directive (the byte-identical `multierr`/`pkg-errors` replaces were already pruned before this change).
- Restrict the config directory to 0700, checksum-verify `go:generate` downloads, and make `scripts/build-macos.sh` safe (mktemp build root, validated caller-supplied VERSION).
- Split `App.vue` state by domain with a typed `ViewContext`; bound frontend retained state (operations map, reconnect backoff); fix frontend hygiene (on-demand component registration, i18n namespace/lang/fallback, dead component removal, bounded SMS list rendering).
- Unify redaction: explicit field allowlist for events and notifications, sensitive data out of default log output, eSIM `matchingID` omitted from download logs, macOS "sender only" notification preference.
- Align cross-path behavior: EF_IMSI decodes identically on AT and MBIM, polling never rewrites operator format selection, the pure-AT eSIM port shares the device-level APDU arbiter, the two AT APDU wrappers merge, MBIM/QMI transport-scope models align or are documented, discovery probing matches the runtime's single-device consumption, rescans serialize with the polling lifecycle, API failures classify explicitly, SMS storage dedupes per SIM identity with pagination, and status polling stops redundant publication/probing.

**Non-Goals:**

- Platform probing behavior that changes runtime semantics is out of scope: the darwin probe-failure cooldown (3.5 M1), the 25-second Linux scan budget (3.5 M3), and the USB-only fallback on tty-probe failure (3.5 M5) remain as-is. Only the contract asymmetry (3.5 M6, probing unused candidates) is addressed.
- Platform correctness fixes with behavioral scope outside consistency are out of scope: interface-name cache TTL (3.5 M2) and the fuser PID-reuse/ownership checks (3.5 M4, 3.2 M11) are not part of this batch.
- eSIM watchdog/lease semantics, read-path cancellation, sentinel-error refactors, `SW1=0x63` classification, and dead `SwitchProfileResult` fields (3.1 H2, M1-M4, M6-M7, L2, L5, L9) belong to the security/reliability batches or are not in this batch's item list.
- No new API endpoints. The `sender_only` notification preference field and the explicitly listed service-error status/code mappings are the only intentional API/wire behavior changes; no other request or success-payload shapes change. No behavior changes to the notification panel slot model (3.9 M5 remains out of scope).
- No new features: this batch is cleanup and consistency only.

## Decisions

### D1. Remove the legacy config surface and viper from the root module

Delete the unreferenced legacy surface in `internal/config`: `Load`/`GetConfig`/`UpdateNotificationInFile` (`config.go:229-279`, `manager.go`, `persist.go`) and the Telegram/Feishu/QQ/Webhook/Bark/Email/Pushplus notification-channel config types, together with `GetConfig`/`GetDeviceByID` returning shared mutable pointers (replace with value copies or removal; verify callers first). Remove `github.com/spf13/viper` from the root `go.mod` and run `go mod tidy`; the report identifies `file-rotatelogs` and `google/uuid` as dead-path imports, but `pkg/logger` rotation (`logger.go:242`) now becomes live once `fix-reliability-and-lifecycle` wires `logger.Setup`, so keep `file-rotatelogs` if `go mod tidy` still requires it and drop only what becomes unreferenced. Create the config/state directory with mode 0700 where it is first created (2.6). The legacy `web.username/password` defaults are removed here as part of deleting the legacy surface; `fix-security-and-data-loss` defers login authentication and does not touch them.

*Alternatives considered*: keeping `GetConfig` as a read-only wrapper — rejected because the report calls the shared-pointer accessors a race hazard and the new binary has no callers; type-sinking viper config structs — rejected as unnecessary once the load surface is gone.

### D2. Remove remaining dead-state subsystems

- `pkg/logger/logger.go:33-57,138-146`: delete `readerIMSIRegistry` and `LookupIMSIByReader` and the branches that consult it (3.7 L4, 3.10 L6). Grep for callers first; the report confirms zero writers.
- `internal/apduarbiter/arbiter.go:276-280`: delete the legacy `AcquireSession`/`AcquireOneShot` interfaces and their `MaxSessions` compatibility field after confirming no root-product production callers; the coordinator-based flow is the only live path. *Ownership*: the arbiter's lease/force-release/watchdog semantics at `arbiter.go:441-443,702-730` are owned by `fix-security-and-data-loss` D5 — this deletion is a disjoint line range and must not touch force-release logic.
- `web/src/components/ErrorState.vue` (0 references) is deleted; `macos/.../NativeUIHost.swift` `webURL` dead code is deleted or replaced with a comment (3.9 L6).
- Frontend dead state: `web/src/stores/device.ts` operations map gains terminal-state removal (see D6).

### D4. Release build script safety

`scripts/build-macos.sh`: replace `BUILD_ROOT="${TMPDIR:-/tmp}"` + `rm -rf` with `mktemp -d`; validate `VERSION` against a strict pattern (e.g. `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`) before any use in PlistBuddy or `PACKAGE_NAME`, rejecting values containing `/` that could write outside `dist`; make `VERSION` a required argument instead of the hardcoded `v0.1.5-preview` fallback (3.10 L1). Keep the existing good practices (SHA-256 verification, `.tmp` atomic download, `set -eu`).

### D5. Checksum-verified `go:generate` for the PKI data

`internal/esim/pki/pki.go:5-6`: pin the SHA-256 of the two source JSON documents (`ci.json`, `accredited.json` from euicc-manual.osmocom.org) next to the generate directive (a small script or a checksum file), download to a `.tmp` file, verify, then atomically rename. Mismatch aborts generation and leaves the committed files untouched (3.10 L6, 2.6). Note in the script how to refresh the checksums deliberately.

### D6. `App.vue` domain split, typed `ViewContext`, and frontend hygiene

- Replace `ViewContext = Record<string, any>` (`web/src/views/context.ts:6`) with a typed interface: the shared surface becomes explicit typed members (device state, capabilities, action invokers, operation tracking, etc.), and routed views receive typed props/slots instead of ad-hoc keys.
- Move view state out of `App.vue` into per-domain Pinia stores (`stores/device.ts` exists; add domain stores for SMS, eSIM, network, VoWiFi, and operations as the split dictates), each owning its API calls and WS event handling. `App.vue` shrinks to shell concerns (navigation, connection, capability gating). Incremental: migrate one domain at a time, keeping the app building and `npm run build`/type-check green at each step (3.8 M4).
- Bound client state (3.8 L1): the operations map in `stores/device.ts` removes entries when their terminal event arrives (or a TTL elapses); the 2.5s fixed reconnect becomes exponential backoff with jitter and a bounded maximum.
- Register Ant Design components on demand (`main.ts:3,10`) via manual imports or the AntDesignVueResolver instead of `app.use(Antd)` (3.8 L2).
- i18n hygiene (3.8 L3/L4): `runVowifi` moves to a dedicated `vowifi` namespace; `watch(locale)` syncs `document.documentElement.lang`; operation/call status renders through the i18n catalog with a fallback so unknown keys never render raw.
- Delete the unreferenced `web/src/components/ErrorState.vue`; render SMS session lists lazily above a threshold instead of mounting all rows (3.8 L5).

### D7. Unified redaction strategy

- **Events** (3.6 L4): replace the CJK-heuristic `publicText` (`internal/api/http/server.go:1225-1255`) with explicit typed projections plus one field allowlist for raw maps. The projections preserve the existing outer shapes of `domain.Snapshot` and `device.Status`; they redact fields inside those shapes rather than flattening a `device.Status` into snapshot fields. The initial table is:

  | Event family | Public fields | Always redacted |
  | --- | --- | --- |
  | `device.status.changed` (`domain.Snapshot`) | `state`, `backend`, `generation`, `capabilities` names, identity, product metadata, radio metrics, `sim` fields | raw error/reason text (replaced with the existing fallback text, no CJK heuristic) |
  | `snapshot` / REST `device.Status` | the nested `snapshot` projection plus `identity` (IMEI/ICCID/IMSI/EID), `radio` metrics, and `sim` fields; preserve the `snapshot`, `identity`, `radio`, and `sim` object shape | raw error/reason text (replaced with fallback text) |
  | `network.updated` | `registered`, `network_mode`, `radio_band`, signal metrics | raw backend payload, subscriber identity |
  | `sms.received` | `index`, `received_at`, `recorded_at` | `body`, sender, recipient, concatenation payload |
  | `call.*` | `id`, `direction`, `state`, `started_at`, `ended_at`, `missed` | phone number, raw modem text |
  | `operation.*` | `operation_id`, `type`, `state`, `progress`, `started_at`, `finished_at` | free-form message, raw error details |
  | `backend.*` and unknown types | no nested data fields | the complete raw `data` value |

  **Scope and real consumers**: the sanitizer applies to the public event stream — WebSocket events and the REST status/snapshot payloads. Device identity (IMEI/ICCID/IMSI/EID) stays public in `device.status.changed` and `snapshot`/REST `device.Status`: the web Overview identity card renders it (client-side masked until the user reveals it, `web/src/App.vue` `maskSensitive`/`showSensitive`), and the WS snapshot feeds the same status state via `applyStatus`, so redacting identity there would break the product's own UI. The temporary loopback + Origin/Host boundary already protects identity from non-local readers. REST data endpoints (SMS list, call history) are not sanitized and remain readable by same-origin clients; the `sms.received`/`call.*` event redaction is a signal-level restriction only, and the web UI keeps displaying full content because it reloads from those REST endpoints. The sanitizer uses typed projections or a field-name allowlist for `map[string]any`; it never passes through an unknown field. Redacted scalar fields are omitted or replaced with the existing typed zero value without changing the outer success-payload shape. The `fix-reliability-and-lifecycle` WebSocket migration preserves the `Event` envelope and calls this shared sanitizer.
- **Logs** (2.7, 3.2 M4): modem logging keeps IMEI/ICCID masked at Info (full values at Debug), and SMS content / USSD text / dialed and incoming numbers are not logged by default — behind an explicit switch if kept at all. `matchingID` is omitted from eSIM download logs entirely (3.1 L3), at most a digest is logged.
- **Notifications** (3.9 M1): add a `sender_only` preference to the macOS notifier; when enabled, notification content carries no message body. The value defaults to `true` on first install and whenever the persisted field is absent; an explicitly persisted `false` remains respected. Presence-aware decoding distinguishes an absent field from an explicit opt-out.
- **Tooltip exposure** (3.9 L3): operator name / network mode in the menu-bar tooltip is a product decision — resolve via OQ1; no redaction machinery added for it.

*Alternatives considered*: regex/positional masking per sensitive class — rejected because the report calls the heuristic fragile and an explicit allowlist is the inverse, auditable approach; per-field wrappers on every log call — rejected in favor of a small set of dedicated sanitizer/log helpers so the policy lives in one place.

### D8. EF_IMSI decode consistency

`internal/modem/commands.go:720-723` strips `imsiStr[1:]` under the parity-bit assumption; per TS 31.102 the standard layout has no parity prefix, so this deletes the MCC's first digit (verified in the report: truncated-before decode is already complete and correct; MBIM never truncates). Fix: after real-device verification of the EF_IMSI bytes-to-IMSI mapping, delete the `[1:]` truncation on the AT path, add a unit test pinning the standard-layout vector (e.g. `460009300011111` decodes with MCC 460, not 600), and add a cross-path test that AT and MBIM produce identical IMSI for the same EF_IMSI content (3.2 H1).

### D9. Remove the COPS side-effect from pure-query polling

`internal/modem/commands.go:62` (`QueryOperator`) unconditionally issues `AT+COPS=3,2` before `AT+COPS?`, silently rewriting the user's format selection on every poll (3.2 M10). The pure-query contract is strict: issue only `AT+COPS?`, parse the format returned by the modem, and return a classified parse error when it is unsupported. An explicit operator-format command remains the only path allowed to issue `AT+COPS=3,2`; polling never changes modem state and never guesses a format after a parse failure.

### D10. Wire the pure-AT eSIM port into the device-level APDU arbiter

`NewATPort` (`internal/esim/at_port.go:24-33`) creates the manager without `APDUArbiter`, so pure-AT eSIM operations bypass the SIM-switch barrier and APDU-idle waits (3.1 M5). Fix both pure-AT construction paths: thread the device-level arbiter instance through the darwin adapter and app assembly, and through the Linux/Windows `serialESIMPortBuilder` in `internal/app/app.go`. `NewATPort` passes it via `ManagerOptions`; verify that all platforms route eSIM operations through the same device arbiter used by the modem manager. *Ownership*: `at_port.go` also gains request-context threading in `fix-reliability-and-lifecycle` task 3.6 — constructor wiring here and context threading there are disjoint edits on the same file; coordinate order, do not touch each other's code.

### D11. Merge the two AT APDU channel wrappers

Consolidate `ModemChannel` (`internal/esim/channel.go`) and `ATSmartCardChannel` (`internal/esim/at_channel.go`) into one shared `driver.SmartCardChannel` implementation whose only difference is the underlying AT command executor; keep both `manager.go:495` and `at_port.go:31` wiring. The merge carries the uniform guards: reject `Transmit` on channel zero (3.1 L8, currently only `ATSmartCardChannel` checks), and promote the per-APDU `regexp.MustCompile` to package-level vars (3.1 L4). Where the report's "simaid 与 esim 的 channel.go" wording refers to a third historical copy, treat that as covered by this consolidation. Add a table test exercising both transports through the single implementation.

### D12. Align the MBIM/QMI APDU transport-scope models

`mbim_apdu_transport.go:84-102` takes `TransportScopeExclusive` per APDU while the QMI transport supports per-channel concurrency (`arbiter.go:902-910`) (3.1 L6). Keep MBIM exclusive for this batch because the current MBIM source does not prove that concurrent logical channels are independently safe. Document the divergence at both transport and arbiter sites, and cover it with tests: QMI may parallelize distinct channels, while MBIM serializes all APDUs and still shares the device-level SIM-switch barrier. *Ownership*: the arbiter's force-release/watchdog behavior is owned by `fix-security-and-data-loss` D5; this decision only documents scope at `arbiter.go:902-910` and does not change lease semantics.

### D13. Discovery probing matches the runtime's single-device consumption

Declare the single-device constraint in the runtime's discovery contract and change platform discovery so only the candidate the runtime will consume is probed (3.5 M6): Linux currently probes every candidate while the runtime uses only `candidates[0]` (`linux/adapter.go:63-65`, `runtime/runtime.go:120`), and darwin stops at the first responder — the contracts must be symmetric. Scope boundary: this is a contract/probing-amount fix only; probe budgets, cooldowns, and fallback behavior (M1/M3/M5) are non-goals and are not touched.

### D14. Serialize rescans with the polling lifecycle

HTTP `rescan` and the polling-loop scan both mutate `r.backend`; a concurrent scan can re-install a closed backend (`runtime/runtime.go:93-95,111-206`, 3.6 L4). Fix: route the HTTP rescan through the same scan function under the same lifecycle lock as the polling loop, and reject/queue a rescan while a scan or close is in progress. The `fix-reliability-and-lifecycle` shutdown change already converges close ordering; this change ensures the scan path itself cannot race.

### D15. Explicit API error classification

`SaveNote` with a missing/over-long iccid and cancelled-operation paths currently surface as generic 500s (`server.go:475-509,421-440,1258-1277`, `extras/service.go:369-371`, 3.6 L1). Fix: application services return `derrors`-classified errors (validation / conflict / cancelled), and the server maps them to explicit structured codes and statuses instead of the `default` → 500 bucket.

### D16. SMS storage deduplication and pagination

`storage/sqlite.go:118-132,306-342`: the SMS uniqueness key gains the SIM identity (migration v3), so an identical message on a second SIM is stored instead of `IGNORE`d. Because SQLite cannot remove the existing table-level unique constraint in place, migration v3 runs transactionally: create a replacement table with the new `(direction, iccid, sender, recipient, body, received_at)` uniqueness key, copy all existing rows and IDs, recreate indexes, swap tables, and record the migration only after the swap succeeds. Existing rows remain intact. The internal `ListSMS(direction, limit, offset)` gains `LIMIT`/`OFFSET`; non-positive limits use a bounded default. Transparency contract: the SMS application service iterates all pages internally so the existing refresh response (`{items: [...]}`) remains complete — no public HTTP query parameters or wire contract are added, and no caller-visible truncation is introduced. A missing SIM identity is not silently normalized to a shared empty key: the caller retains the modem entry and retries identity acquisition. *Ownership*: the SMS delivery pipeline (consumer, ack, reassembly) is owned by `fix-security-and-data-loss` D3; this change owns storage persistence only and updates that change's SMS service caller when `InsertSMS`/`ListSMS` signatures change.

### D17. Status polling deduplication and firmware cache

- The network/traffic poller publishes a traffic event only when sampled values change (`network/service.go:82-93,113`, 3.6 L3).
- Firmware status is served from a short-TTL cache (order of 1-2 s) instead of running the 4-AT + ADB probe sequence on every read (`firmware/service.go:182-261`, 3.6 L3).

### D18. Native UI hygiene

- Cache the `ISO8601DateFormatter` instances instead of creating two per date decode, and avoid re-encoding already-parsed event JSON (`BridgeModels.swift:165-215`, 3.9 L1).
- Replace the bare `MainActor.assumeIsolated` (`NativeUIHost.swift:23`) with an explicit thread check that produces a readable error, and document the Go LockOSThread contract in `bridge.h` (3.9 L2).
- Incoming call notifications use `.timeSensitive` interruption level (3.9 L4).
- Implement `applicationSupportsSecureRestorableState` returning `true` (3.9 L5).
- Remove the unused `webURL` parse (3.9 L6).

## Risks / Trade-offs

- [EF_IMSI change ships a wrong layout assumption] → Real-device verification gates the change (D8); the unit and cross-path tests pin the standard layout, and MBIM's non-truncating behavior is the reference.
- [Viper/config removal breaks a still-live consumer] → Every removed symbol is grep-verified caller-free before deletion; `go build ./...` + `go vet ./...` and `go mod tidy` are part of the task's definition of done.
- [Replace pruning breaks CI caches or vendoring] → The change is limited to the root go.mod/go.sum; genuine root-product forks stay replaced with a recorded patch note, and the tasks re-run the full build and test suites.
- [Redaction hides useful diagnostics] → Full data remains available at Debug level or under the explicit content-logging switch; the allowlist is per-event-type and auditable.
- [Sender-only notification default changes UX] → The default is explicitly enabled when the field is absent because SMS bodies often contain one-time codes; users can persist an explicit opt-out, and the changelog notes the behavior.
- [`App.vue` split is a large churn in one batch] → Domain-by-domain migration keeps the app building at each step; the typed `ViewContext` lands first so views migrate against a stable contract.
- [A modem needs a specific COPS format for parsing] → The pure-query parser accepts the modem's reported format; an explicit caller command can set a format before a user-requested operation. Polling never mutates the selection and never silently substitutes a format after a parse failure.
- [Arbiter coverage on the darwin pure-AT path could serialize unrelated operations] → That is the intended contract (device-level arbitration); the shared instance is the same one the modem path uses, so contention semantics are unchanged from the modem path.
- [MBIM exclusive scope reduces throughput] → The conservative scope is explicit and testable; it can be relaxed in a later change once the transport proves independent channel safety.

## Migration Plan

- Land in dependency order, each unit independently reviewable and revertible: (1) dead-code removal D1-D2 (config surface, IMSI registry, legacy arbiter interfaces, root replace documentation) — verify with `go build ./...` from root, `go vet ./...`, `go mod tidy`; (2) build hygiene D4-D5; (3) cross-path consistency D8-D12 (EF_IMSI fix gated on device verification, COPS, arbiter wiring, wrapper merge, explicit MBIM scope) — verify with `go test -race` over modem, esim, apduarbiter; (4) redaction D7 (events, logs, notifications) — coordinate with the `fix-reliability-and-lifecycle` WS migration so the allowlist lands on the shared sanitizer once; (5) `App.vue` split D6 — `npm run build` and type-check after each domain; (6) runtime/API/storage/polling hygiene D13-D17 — SQLite migration v3 runs transactionally on open and is tested from a real v2 schema; (7) native UI hygiene D18.
- Rollback: each unit reverts independently; no unit adds an endpoint, and the `sender_only` preference field plus explicit error-code mappings are the intentional wire behavior changes. The SQLite migration is transactional and preserves existing rows. Rolling back across migration v3 requires restoring the pre-migration database backup; no legacy-schema compatibility fallback is added to the new runtime.
- Coordination with siblings: do not touch the auth/WS-upgrade code owned by `fix-security-and-data-loss`, the gorilla migration or shutdown code owned by `fix-reliability-and-lifecycle`, or the `macos-native-ui`/`device-logging`/`device-events`/`device-services`/`modem-backends` requirement texts those changes added; all deltas in this change are additive to theirs. Remove the legacy `web.username/password` defaults here only if they remain after the security change; no compatibility default is retained.

## Open Questions

- OQ1 (3.9 L3): menu-bar tooltip / accessibility label exposes operator name and network mode — confirm the product decision to keep and document, or hide.
- OQ3 (3.2 H1): the real-device EF_IMSI byte-to-IMSI verification is a merge gate for D8; until it is performed the truncation removal stays behind the test pins.
- OQ4 (3.10 L5): exact list of byte-identical root replace directives at apply time; the audit determines the final keep/remove list.
