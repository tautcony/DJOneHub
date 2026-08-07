## Why

The eSIM workbench currently takes roughly 13 seconds to become fully available because a cold overview performs hardware detail reads that the API does not return, notification listing scans every static AID, and the frontend waits for overview, health, and notifications in a waterfall. The manager records discovered eUICCs but deliberately ignores them when selecting later AIDs, so repeated reads continue to pay avoidable logical-channel and APDU costs.

## What Changes

- Add a validated discovered-AID fast path that tries known eUICC targets first, falls back to one full static scan on failure, and invalidates targets across modem/SIM reset boundaries.
- Keep LPA clients and logical channels operation-scoped; do not introduce a permanently open card session.
- Split the public Profile overview read from rich eUICC/product enrichment so `/api/v1/esim` reads only the EID, basic Profile fields, and live active ICCID required by its response.
- Restrict notification listing to discovered eUICC targets, with full discovery fallback when no valid target is known.
- Make initial eSIM UI loading progressive: render Profiles after the overview completes, load local notes independently, and defer health and notification reads so they do not gate the view.
- Add focused timing logs and tests for fast-path, fallback, invalidation, multi-eUICC, cancellation, and frontend load ordering.
- Preserve all existing public eSIM endpoint and response contracts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `device-services`: eSIM reads use validated discovered targets, operation-scoped sessions, a lightweight Profile snapshot, and cancellable fallback discovery.
- `esim-notifications`: pending notification listing reads discovered eUICC targets instead of every static candidate while retaining multi-eUICC coverage and fallback behavior.
- `vue-management-ui`: the eSIM route exposes Profile data without waiting for auxiliary health and notification requests.

## Impact

- Primary backend changes: `internal/esim/manager.go`, `internal/esim/at_port.go`, and their focused tests.
- Application/API contracts remain stable; internal port behavior may be consolidated to avoid repeated overview reads.
- Frontend changes: `web/src/stores/esim.ts` and the eSIM view loading orchestration in `web/src/App.vue`.
- Existing tests that require every read to ignore discovered AIDs must be replaced with fast-path plus fallback assertions.
- No new dependency, database migration, public endpoint, or permanently open logical channel is introduced.
