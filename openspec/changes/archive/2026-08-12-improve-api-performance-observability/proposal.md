## Why

The API registers routes and OpenAPI paths in separate lists. Cross-cutting HTTP
behavior also depends on handler code. A change to deadlines, safe logging, or
metrics can therefore require edits to many endpoints.

Application services implement similar cache, coalescing, generation, and
invalidation logic in different forms. This duplication increases the test
scope and makes policy values difficult to compare or adjust.

The previous proposal combined these concerns with backend, polling, storage,
operation retention, and stream changes. That scope was too large for one
change.

## What Changes

- Add one typed route registry for HTTP method, canonical path template,
  workload class, stream kind, handler, and OpenAPI metadata.
- Add centralized workload policies for request deadlines, safe completion
  logs, and bounded route metrics.
- Add one generic application snapshot component for time-to-live (TTL),
  generation scope, cache epoch, shared loads, load deadlines, and cache
  outcomes.
- Keep invalidation decisions in the owning application service. Mutations use
  explicit invalidation calls with fixed reasons.
- Move compatible existing caches into the snapshot component. This migration
  includes Device status components, the application eSIM overview, Device
  Control stable status, and active ICCID memoization.
- Use the snapshot component for the new pending eSIM notification snapshot.
- Keep mutable read models, protocol discovery state, retry throttles, and event
  deduplication outside the snapshot component.
- Preserve all public `/api/v1` schemas, capability checks, transport
  arbitration, and operation behavior.
- Defer AT timing correlation, SMS refresh changes, Network status caching, traffic
  checkpoints, operation retention, and WebSocket snapshot changes to separate
  changes.

## Capabilities

### New Capabilities

- `api-performance-observability`: Defines route metadata, workload policies,
  safe route metrics, and reusable application snapshot behavior.

### Modified Capabilities

- `device-api`: Requires one route registry and workload-specific request
  deadlines without changes to public API schemas.
- `device-services`: Requires compatible existing application caches and the
  new pending notification cache to use the reusable snapshot behavior.
- `esim-notifications`: Requires pending notification reads to use the reusable
  snapshot behavior and explicit invalidation.
- `device-logging`: Requires API completion logs to use canonical route
  templates and allowlisted fields.

## Impact

- Affected HTTP code: `internal/api/http` and server construction in
  `cmd/djonehub`.
- Affected application code: the shared snapshot package and the Device, eSIM,
  Device Control, and Network services.
- Affected tests: route registry invariants, snapshot component behavior,
  existing-cache migration tests, and focused adoption tests.
- Affected documentation: OpenAPI generation and the diagnostics code map.
- No database migration is required.
- No backend contract change is required.
- No frontend change is required.
