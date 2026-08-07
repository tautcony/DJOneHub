## 1. Discovered AID Fast Path

- [x] 1.1 Replace the full-static-only AID plan with a normalized discovered-target fast path and retain a full-static plan for fallback.
- [x] 1.2 Add one-shot fallback discovery when preferred targets yield no readable eUICC, preserving distinct SE0/SE1 EIDs and operation-scoped client cleanup.
- [x] 1.3 Update reset and mutation invalidation tests and replace full-static-only assertions with fast-path, stale-target fallback, and multi-eUICC coverage.

## 2. Lightweight Profile Snapshot

- [x] 2.1 Add an independently cached and singleflight-coalesced lightweight snapshot containing EID groups and basic Profiles without rich eUICC/product enrichment.
- [x] 2.2 Route the AT eSIM port EID/Profile overview behavior through the lightweight snapshot while preserving active ICCID reconciliation and public API output.
- [x] 2.3 Add tests proving public Profile overview skips rich/product reads, shares its snapshot, honors cancellation, and invalidates after reset/Profile mutations.

## 3. Notification Targeting

- [x] 3.1 Change notification listing without an explicit AID to use distinct discovered eUICC targets and perform one discovery fallback when targets are missing or stale.
- [x] 3.2 Add notification tests for a single eUICC alias, eSTK Max dual EIDs, stale-target recovery, ordering, and structured failure behavior.

## 4. Progressive Frontend Loading

- [x] 4.1 Refactor the eSIM store so overview publication is not blocked by notes or health, and auxiliary failures preserve valid Profile state.
- [x] 4.2 Stop initial route completion from waiting for notifications while retaining explicit and background notification refresh behavior.
- [x] 4.3 Add or update frontend tests/type checks for progressive load ordering and isolated auxiliary state.

## 5. Diagnostics and Verification

- [x] 5.1 Add structured aggregate timing logs for AID policy/fallback, lightweight Profile reads, and notification reads without logging sensitive values.
- [x] 5.2 Run focused eSIM manager/application/API tests and validate the OpenSpec change.
- [x] 5.3 Run the full Go test suite plus frontend typecheck, lint, and production build.
