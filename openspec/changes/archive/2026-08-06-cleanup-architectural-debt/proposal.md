## Why

The third review batch (docs/code-review-report.md items 11-14) targets architectural debt in the root product and the two earlier fix batches: dead code surfaces (the viper-backed config loader, the IMSI registry, replace directives that patch nothing), a 1396-line `App.vue` god component with an untyped `ViewContext`, an ad-hoc sensitive-data redaction strategy (content heuristics instead of an explicit allowlist), and cross-path inconsistencies (EF_IMSI truncation on the AT path only, a pure-AT eSIM port outside the APDU arbiter, two near-identical AT APDU wrappers, and a side-effecting `AT+COPS=3,2` in a pure-query poll). None of these crash or lose data, but they keep dead code compiled into the binary, make behavior diverge between paths, and block maintenance.

## What Changes

- Remove dead code and prune the root build: delete the legacy viper-backed config surface (`Load`/`GetConfig`/`UpdateNotificationInFile` and the Telegram/Feishu/QQ/Webhook/Bark/Email/Pushplus notification-channel configs) and the shared-pointer accessors, remove the IMSI reader registry in `pkg/logger`, remove the legacy `AcquireSession`/`AcquireOneShot` arbiter interfaces (no root-product callers), remove viper from the root `go.mod`, document the patch purpose of every remaining root replace directive (byte-identical ones were already pruned), restrict the config directory to `0700`, checksum-verify the `esim/pki` `go:generate` downloads, and harden `scripts/build-macos.sh` (mktemp build root, validated and caller-supplied VERSION).
- Split `App.vue` state by domain: replace the `Record<string, any>` `ViewContext` with typed interfaces, move view state into per-domain Pinia stores, bound the operations map and reconnect backoff, register Ant Design components on demand, and fix i18n hygiene (locale-synced `lang`, dedicated vowifi namespace, status-key fallback).
- Unify sensitive-info redaction: replace the CJK-replacement `publicText` heuristic with an explicit field allowlist applied to the public event stream (WS events + REST status/snapshot), keeping device identity present in status/snapshot payloads (the web Overview card renders it, client-side masked) and leaving REST data endpoints (SMS list, call history) unsanitized; keep IMEI/ICCID, SMS content, USSD text, and dialed/incoming numbers out of Info-level logs (masked or behind a switch); omit the eSIM `matchingID` from download logs; add a "sender only" preference for macOS notifications.
- Align cross-path behavior: fix EF_IMSI decoding on the AT path (drop the spurious first-digit truncation so both AT and MBIM return the full IMSI), stop the pure-query poll from issuing the side-effecting `AT+COPS=3,2`, wire the pure-AT eSIM port into the device-level APDU arbiter, merge the two AT APDU channel wrappers into one implementation, document the deliberate MBIM-exclusive versus QMI-per-channel APDU scope, declare the single-device constraint so platform discovery probes only the candidate the runtime consumes, serialize rescans with the polling lifecycle, map service failures to explicit structured error codes instead of generic 500s, deduplicate SMS storage per SIM identity with bounded internal pagination, and stop publishing unchanged traffic/firmware status.

## Capabilities

### New Capabilities

- `build-hygiene`: root-product dependency and build-script hygiene — removal of the legacy config surface and other dead state, documented root `go.mod` replaces, checksum-verified code generation, restrictive config-directory permissions, and safe release build scripts.
- `macos-native-ui` (no baseline; introduced by `fix-security-and-data-loss`, extended here): notifications suppress message content per preference and mark time-sensitive events; the bridge threading contract is verified explicitly instead of assumed.
- `device-logging` (no baseline; introduced by `fix-reliability-and-lifecycle`, extended here): device-layer logs keep sensitive data (IMEI/ICCID, message and USSD content, call numbers, eSIM activation code) out of default output.

### Modified Capabilities

- `device-api`: service-level failure classification (validation failures and cancelled operations map to explicit structured codes instead of a generic 500).
- `device-events`: public event stream payloads are sanitized by an explicit field allowlist instead of the CJK-replacement `publicText` heuristic; device identity stays public in status/snapshot payloads.
- `device-services`: SMS storage deduplicates per SIM identity with bounded internal listing; status polling stops redundant publication and probing.
- `modem-backends`: EF_IMSI decodes consistently across AT and MBIM paths; status polling no longer rewrites operator format selection; every APDU transport is coordinated by the device-level arbiter through one shared AT APDU channel implementation.
- `single-device-runtime`: platform discovery probes only the candidate the runtime consumes (single-device constraint declared); rescans are serialized with the polling lifecycle.
- `vue-management-ui`: view state is domain-owned with typed view context; retained client state stays bounded; localization is consistent (lang, namespaces, fallbacks).

## Impact

- Go: `internal/config`, `pkg/logger`, `internal/apduarbiter`, `internal/modem` (EF_IMSI decode, COPS polling), `internal/esim` (channel merge, AT port arbiter wiring), `internal/runtime` (scan serialization), `internal/api/http` (error mapping, event sanitization), `internal/application` (network/firmware polling hygiene), `internal/backend` (MBIM IMSI path), `internal/storage` (bounded SMS listing), `internal/platform` (all pure-AT eSIM port wiring, discovery contract), and the root `go.mod`.
- Web: `web/src/App.vue`, `web/src/views/context.ts`, `web/src/stores/`, `web/src/main.ts`, i18n resources.
- macOS Swift: `macos/DJOneHubNotifier` (notification content preference, interruption level, thread check, dead code).
- Build: `scripts/build-macos.sh`, `internal/esim/pki` `go:generate` (checksum verification).
- Storage/API: SQLite schema migration v3 (SMS uniqueness key includes SIM identity) and bounded internal SMS listing; no new public endpoints. The `sender_only` notification preference field and explicit structured error codes/statuses are the intentional wire/API behavior changes.
