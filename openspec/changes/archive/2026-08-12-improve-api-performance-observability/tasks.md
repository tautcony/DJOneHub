## 1. Route Policy Foundation

- [x] 1.1 Add typed route, workload, stream, and OpenAPI operation metadata.
- [x] 1.2 Define one route registry for every registered `/api/v1` method.
- [x] 1.3 Register `http.ServeMux` handlers from the route registry without
  changing handler behavior.
- [x] 1.4 Generate OpenAPI path operations from the route registry.
- [x] 1.5 Add invariant tests for duplicate routes, method coverage, prefix
  templates, multi-method resources, and OpenAPI coverage.

## 2. HTTP Policy Enforcement

- [x] 2.1 Add the central workload policy table and initial request deadlines.
- [x] 2.2 Apply workload deadlines to non-stream routes.
- [x] 2.3 Preserve the existing WebSocket and Server-Sent Events deadline rules.
- [x] 2.4 Add bounded route counters and duration histograms.
- [x] 2.5 Change completion logs to use canonical route templates and allowlisted
  outcome fields.
- [x] 2.6 Add tests that prove paths, queries, payloads, identifiers, commands,
  responses, and raw error text do not enter route metrics or logs.

## 3. Generic Snapshot Component

- [x] 3.1 Add a generic snapshot component with a typed policy, scope, outcome,
  clone function, and explicit invalidation reason.
- [x] 3.2 Implement TTL, generation, epoch, and successful-value storage rules.
- [x] 3.3 Implement one shared load for each generation and epoch.
- [x] 3.4 Make caller cancellation stop only that caller's wait.
- [x] 3.5 Run each shared load from a service lifecycle context with a finite load
  deadline.
- [x] 3.6 Reject late writes after generation change or invalidation.
- [x] 3.7 Add focused tests for hits, misses, stale values, coalesced callers,
  cancellation, timeout, errors, cloning, invalidation, and generation races.

## 4. Existing Cache Consolidation

- [x] 4.1 Migrate Device status component caches without changing backend query
  contracts or public response fields.
- [x] 4.2 Migrate the application eSIM overview cache. Preserve its ten-second
  TTL, runtime generation, cloning, and current invalidation boundaries.
- [x] 4.3 Migrate the Device Control stable probe cache and merge current EDL
  session state after the snapshot read.
- [x] 4.4 Migrate Network active ICCID memoization. Preserve its current lookup
  behavior and add runtime-generation scope without adding Network status cache.
- [x] 4.5 Remove replaced mutex, timestamp, epoch, and singleflight fields. Do not
  keep parallel fallback caches in the migrated services.
- [x] 4.6 Add regression tests for TTL values, clones, invalidation boundaries,
  generation changes, error behavior, and unchanged public responses.

## 5. New Snapshot Adoption

- [x] 5.1 Add pending eSIM notifications as a five-second snapshot.
- [x] 5.2 Add explicit pending-notification invalidation after process, removal,
  eSIM Profile mutation, reset, card change, and validated target failure.
- [x] 5.3 Add tests for concurrent callers, cancelled waiters, failed loads,
  mutation invalidation, generation changes, and unchanged response schemas.

## 6. Documentation And Verification

- [x] 6.1 Update the API and diagnostics code maps for the route registry,
  workload policies, and snapshot component.
- [x] 6.2 Update the cache inventory if implementation discovers another
  cache-shaped object in the affected packages.
- [x] 6.3 Record final policy values and read-only measurements in
  `docs/api-performance-audit.md`.
- [x] 6.4 Run focused Go tests for HTTP, Device, eSIM, Device Control, and Network.
- [x] 6.5 Run race tests for route summaries, snapshots, and migrated services.
- [x] 6.6 Search migrated services for replaced cache fields and parallel paths.
- [x] 6.7 Run `go test ./...` and `openspec validate
  "improve-api-performance-observability" --strict`.
- [x] 6.8 Perform read-only real-device measurements. Do not change SIM, eSIM,
  firmware, USB mode, or network state.

## Deferred Changes

The following work requires separate OpenSpec changes after adoption measurements:

- Combined backend Device status sampling and AT timing correlation.
- SMS refresh and polling changes.
- Network status caching and traffic checkpoints. Active ICCID consolidation is
  part of this change.
- Operation retention and operation timing.
- WebSocket initial snapshot behavior.
- External probe instrumentation.
