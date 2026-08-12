## Context

`internal/api/http/server.go` registers each path directly on `http.ServeMux`.
`internal/api/http/openapi.go` keeps a second path list. Method checks, request
logging, and future deadline selection remain separate from both lists.

The Device, eSIM, Device Control, and Network services implement compatible
cached reads. Their implementations use different combinations of TTL,
generation, singleflight, cloning, and invalidation state. These differences
make common safety rules difficult to verify.

Other code uses the word `cache` for mutable read models, protocol discovery,
event deduplication, retry throttles, and presentation state. These objects do
not have the same contract as an immutable snapshot. The design must not force
them into one abstraction.

The application manages one local device. A shared abstraction must preserve
runtime generation, resource arbitration, caller cancellation, and finite
device deadlines. It must not make cache policy an HTTP concern.

## Goals

- Make HTTP policy declarative and centrally adjustable.
- Remove duplicate route and OpenAPI path declarations.
- Provide one tested implementation for reusable snapshot behavior.
- Keep business invalidation rules visible in each owning service.
- Move all compatible existing application caches into the shared component.
- Record why each incompatible cache remains outside the shared component.
- Limit new cache behavior to pending eSIM notification reads.
- Preserve current API schemas and device safety behavior.

## Non-Goals

- Add language-level annotations, reflection, or code generation.
- Put application cache rules in route metadata.
- Convert mutable state or protocol-specific caches to the snapshot component.
- Cache mutations, raw AT, connectivity checks, or explicit diagnostics.
- Change AT backend commands or add request-to-AT timing correlation.
- Change SMS polling, Network status caching, traffic persistence, operation
  retention, or event stream initialization.
- Add a telemetry exporter or a new third-party metrics dependency.

## Design Overview

The design has two independent policy layers:

```text
HTTP request
    |
    v
RouteSpec + WorkloadPolicy
    |  method, path template, request deadline, logging, metrics, OpenAPI
    v
Application handler
    |
    v
Snapshot[T] + SnapshotPolicy
       TTL, generation, epoch, shared load, load deadline, outcome
```

The route layer controls HTTP behavior. The snapshot layer controls reusable
application reads. A route does not select or invalidate an application cache.

## Decisions

### Use a typed route registry instead of annotations

Go does not provide native annotations. Struct tags would require reflection
and would not type-check handler references. Code generation would add a build
step for a small local API.

The HTTP package will define typed route metadata:

```go
type RouteSpec struct {
    Method      string
    Pattern     string
    Workload    WorkloadClass
    Stream      StreamKind
    Handler     HandlerRef
    Operation   OpenAPIOperation
}
```

`HandlerRef` can bind a `Server` method without changing each handler body. The
registry will be the source for dispatch and OpenAPI path generation.

The registry will not contain cache TTL, generation, coalescing, or invalidation
fields. Those fields describe application data, not HTTP routes.

### Use workload classes for HTTP policy

Each route selects one low-cardinality workload class. One central policy table
maps the class to a request deadline and metric class.

The initial classes are:

| Workload class | Intended work | Initial deadline |
| --- | --- | --- |
| `memory_read` | Bounded in-memory state | 5 seconds |
| `storage_read` | Bounded local SQLite read | 5 seconds |
| `device_read` | Normal device read | 30 seconds |
| `full_device_read` | Full eSIM or explicit device read | 45 seconds |
| `local_command` | Synchronous local mutation | 30 seconds |
| `async_accept` | Validation and operation acceptance | 5 seconds |
| `external_probe` | Endpoint-owned external work | 45 seconds |
| `stream` | WebSocket or Server-Sent Events | No route deadline |

These values are defaults. A later measured change can adjust the policy table
without editing endpoint handlers.

The server will not set a global response write timeout. Stream routes retain
their existing keepalive and write-deadline behavior.

### Keep route instrumentation bounded and payload-free

Route middleware will record these fields:

- HTTP method.
- Canonical path template.
- Workload class.
- Status class.
- Structured error code when available.
- Response byte count.
- Total duration bucket.

The middleware will not record concrete paths, query strings, request bodies,
response bodies, identifiers, credentials, commands, responses, or error text.

The first implementation will retain fixed counters and histograms only. It
will not retain one record for each request. Detailed application and AT phase
correlation is outside this change.

### Generate OpenAPI paths from route metadata

Each `RouteSpec` contains its OpenAPI operation metadata. OpenAPI generation
will group entries by path template and method. This supports resources that
use more than one method.

Shared schemas and responses remain in the existing OpenAPI builder. The route
registry replaces only the duplicate path and method declarations.

An invariant test will fail for duplicate method and template pairs. Another
test will compare the registered route set with generated OpenAPI operations.

### Add one reusable application snapshot component

A dependency-neutral application package will provide a generic component:

```go
type SnapshotPolicy struct {
    Name        CacheName
    TTL         time.Duration
    LoadTimeout time.Duration
}

type Scope struct {
    Generation uint64
    Epoch      uint64
}

type Snapshot[T any] struct { /* private state */ }

func (s *Snapshot[T]) Get(
    ctx context.Context,
    scope Scope,
    load func(context.Context) (T, error),
) (T, Outcome, error)

func (s *Snapshot[T]) Peek(scope Scope) (T, bool)
func (s *Snapshot[T]) Invalidate(reason InvalidationReason)
```

The final package and type names may follow existing repository naming. The
behavioral contract is authoritative.

The component will provide these rules:

- A cached value is valid only for its generation and epoch.
- A cached value expires after its TTL.
- One load runs for the same generation and epoch.
- Each caller can stop waiting when its context ends.
- One caller cancellation does not cancel the shared load.
- The shared load uses a service-owned parent context and `LoadTimeout`.
- A late result cannot populate a different generation or invalidated epoch.
- An error does not become a successful cache value.
- Returned mutable values use a service-provided clone function when required.
- Each call reports `hit`, `miss`, `stale`, or `coalesced`.

The component will not subscribe to runtime events. The service supplies the
current generation and calls `Invalidate` at business mutation boundaries.

### Keep invalidation explicit in application services

Invalidation cannot be derived safely from an HTTP method or route name. A
mutation can affect more than one snapshot. Background work can also change
device state without an HTTP request.

Each owning service will call `Invalidate` after the established success
boundary. The call will use a fixed reason for diagnostics and tests. The
reason will not contain device data or request values.

Examples include:

- Device runtime generation change.
- Device Control mutation or settings change.
- Pending notification process or removal.
- eSIM Profile mutation.
- Confirmed card or modem reset boundary.

Cross-service invalidation wiring is limited to existing service boundaries.
This change will not add a global invalidation event language.

### Consolidate compatible existing caches

An existing cache must move to `Snapshot[T]` when it has all these properties:

- It stores a successful read result.
- Callers treat the stored value as an immutable snapshot.
- It has a TTL or an explicit invalidation boundary.
- It can use generation and epoch scope without changing business behavior.
- It can return a clone when its value contains mutable data.

The migration includes these existing implementations:

| Existing implementation | Current behavior | Migration |
| --- | --- | --- |
| Device Identity, Radio, and SIM component caches | TTL, runtime generation, and singleflight | Use three typed snapshots and preserve current TTL values |
| Application eSIM overview cache | 10-second TTL, runtime generation, epoch, singleflight, and clone | Use one typed snapshot and preserve all invalidation boundaries |
| Device Control status cache | 1.5-second TTL and explicit invalidation | Cache stable probe data by runtime generation and merge volatile state later |
| Network active ICCID memoization | 15-second positive TTL | Use a typed snapshot with runtime generation and preserve current lookup behavior |

The active ICCID migration does not add Network status caching. It only removes
the local `iccid` and `iccidChecked` cache implementation.

Pending eSIM notifications do not have an application cache today. They are the
only new snapshot behavior in this change.

### Keep incompatible cache-shaped state with its owner

The following state will not move to `Snapshot[T]`:

| State | Reason |
| --- | --- |
| SMS inbox and sent-message state | It is a bounded mutable read model that merges modem events and SQLite records |
| SMS fragment reassembly | It is a keyed collector with per-fragment expiry |
| Call startup and active-call state | It is event-driven lifecycle state |
| Notification seen and delivered sets | They provide event deduplication and delivery idempotency |
| eSIM Manager overview, Profile, chip, and discovered-AID state | The manager patches related values and owns protocol discovery and write recovery |
| eSIM card-probe failure cooldown | It is negative protocol retry control |
| Device Control EDL observations | They use source-specific reuse windows and mutable session state |
| ADB server retry suppression | It is a process-wide retry throttle |
| Platform interface-name caches | They belong to platform discovery and require platform validation |

These exclusions prevent the generic component from becoming a general state
container. A later change can add a different abstraction for a specific state
category.

### Separate the request deadline from the load deadline

`WorkloadPolicy` limits how long one HTTP request can wait. `SnapshotPolicy`
limits how long shared application work can run. These deadlines have different
owners and must not share one context.

The shared load will use the runtime session or service lifecycle context. The
component will add `LoadTimeout` to that parent. The caller context controls
only that caller's wait.

### Adopt the component without parallel cache implementations

This change includes:

1. Device status component snapshots.
2. Application eSIM overview.
3. Device Control stable probe status.
4. Network active ICCID lookup.
5. Pending eSIM notifications.

Device status keeps its current component loading behavior. A combined backend
snapshot port is outside this change. The application eSIM overview keeps its
current ten-second TTL and invalidation rules. Device Control will cache stable
probe data and merge volatile EDL session state after the snapshot read. The
active ICCID lookup keeps its current positive-cache behavior and gains runtime
generation scope. Pending eSIM notifications will use a five-second snapshot
and its existing application service API.

Each migrated service must remove its old mutex, timestamp, singleflight, and
epoch fields when the generic component owns those concerns. It must not keep a
parallel fallback cache. The adoption must not change public response fields or
claim that a cache hit is a new hardware verification.

### Keep explicit live paths outside snapshots

The following work will not use the generic snapshot component:

- State-changing operations.
- Raw AT requests.
- Connectivity checks.
- Explicit diagnostics.
- Operation status lookup.
- Simple bounded storage reads.
- WebSocket and Server-Sent Events connections.

## Migration Strategy

1. Add route and workload types with registry invariant tests.
2. Move existing route registration and OpenAPI path metadata into the
   registry without handler behavior changes.
3. Add route deadlines, safe logging, and bounded route metrics.
4. Add and test the generic snapshot component.
5. Migrate the existing Device and application eSIM overview caches.
6. Migrate the existing Device Control and active ICCID caches.
7. Add the pending eSIM notification snapshot.
8. Search the affected services for old cache fields and parallel cache paths.
9. Measure the adopted snapshots before a later change selects more services.

Each step must keep the repository buildable. Route migration and snapshot
migration do not need to occur in the same commit.

## Risks And Controls

- A registry conversion can omit a route. The route and OpenAPI invariant tests
  compare complete method and path sets.
- A shared load can continue after all callers leave. The service lifecycle and
  `LoadTimeout` keep the load bounded.
- A stale load can write after invalidation. The component checks generation
  and epoch before it stores a result.
- A mutable cached value can leak changes between callers. The service supplies
  a clone function for maps, slices, pointers, and nested mutable values.
- A route deadline can cancel slow verified hardware. Workload values remain
  centralized and must be measured before reduction.
- A generic component can hide business rules. Services keep loaders and
  invalidation calls explicit.
- A broad consolidation can absorb unrelated state. The cache inventory gives
  each existing cache-shaped object an explicit migration decision.

## Open Questions

- The implementation must select the service lifecycle context that owns each
  shared load. It must not use `context.Background()`.
- Real-device read-only measurements must confirm the initial device deadlines.
- A later change can decide whether SMS reconciliation needs a no-TTL shared
  flight or a separate abstraction.
