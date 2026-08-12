## ADDED Requirements

### Requirement: Reusable snapshots SHALL protect generation and invalidation boundaries

The application snapshot component SHALL scope successful values to a runtime
generation and cache epoch. A late result SHALL NOT populate a different scope.
One shared load SHALL serve concurrent callers for the same scope. A cancelled
caller SHALL stop waiting without cancelling the shared load for other callers.
Errors SHALL NOT become successful cache entries.

#### Scenario: Device reconnects during a load
- **WHEN** the runtime generation changes before a shared load completes
- **THEN** the completed value does not populate the new generation

#### Scenario: First waiter disconnects
- **WHEN** one caller disconnects while another caller waits for the same read
- **THEN** the second caller can receive the bounded shared result

### Requirement: Compatible existing caches SHALL use explicit snapshot policies

Device status components, the application eSIM overview, Device Control stable
probe status, and Network active ICCID memoization SHALL move from local cache
implementations to the reusable snapshot component. Each service SHALL preserve
its existing TTL, cloning, lookup, and invalidation behavior unless this change
specifies a stronger generation rule. The owning service SHALL keep its loader
and invalidation calls explicit. Volatile session state SHALL remain outside
stable cached values.

#### Scenario: Device Control status is warm
- **WHEN** a second status request arrives within the cache TTL and generation
- **THEN** it reuses stable probe data and merges current session state

#### Scenario: Pending notification is removed
- **WHEN** a pending notification removal succeeds
- **THEN** the eSIM service invalidates its pending snapshot before the next list response

#### Scenario: Existing eSIM overview is warm
- **WHEN** the application eSIM overview remains within its ten-second TTL
- **THEN** the service returns a clone from the reusable snapshot without a new card read

#### Scenario: Existing active ICCID value is warm
- **WHEN** Network requests the active ICCID within its current positive TTL
- **THEN** the service uses the reusable snapshot without adding a Network status cache

### Requirement: Migrated services SHALL remove parallel cache implementations

A service that moves an existing cache to the reusable snapshot component SHALL
remove the replaced mutex, timestamp, generation, epoch, and singleflight state.
It SHALL NOT retain a parallel fallback cache for the same read.

#### Scenario: Migration is reviewed
- **WHEN** the implementation of a migrated service is inspected through compiled tests and code review
- **THEN** one snapshot component owns the shared cache mechanics for that read

### Requirement: Snapshot policy SHALL be independent from HTTP policy

An application snapshot load SHALL use its service lifecycle context and its own
finite load deadline. It SHALL NOT use the first HTTP caller context as the
shared load context.

#### Scenario: HTTP request is cancelled
- **WHEN** the first HTTP waiter is cancelled during a shared load
- **THEN** another active waiter can still receive the bounded load result
