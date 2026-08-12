## ADDED Requirements

### Requirement: Route performance records SHALL use safe bounded dimensions

The HTTP layer SHALL record method, canonical route template, workload class,
status class, structured error code, response size, and bounded duration data.
It SHALL NOT record concrete paths, query strings, request or response bodies,
device identifiers, subscriber identifiers, phone numbers, credentials,
commands, responses, or user filesystem paths.

#### Scenario: Request path contains an identifier
- **WHEN** a client requests a path that contains an ICCID or operation ID
- **THEN** the performance record contains only the registered route template

### Requirement: Route performance summaries SHALL remain bounded

The runtime SHALL retain fixed counters and duration histograms by allowlisted
low-cardinality route dimensions. It SHALL NOT retain one performance object for
each request.

#### Scenario: Process handles many requests
- **WHEN** the process handles requests for longer than the application life
- **THEN** performance memory remains bounded independently of request count

### Requirement: Application snapshots SHALL report cache outcomes

The reusable application snapshot component SHALL report `hit`, `miss`,
`stale`, and `coalesced` outcomes using fixed cache names. It SHALL not expose
cached values as proof of a new live hardware read.

#### Scenario: A warm adopted snapshot returns
- **WHEN** an adopted service returns a current-generation cached value
- **THEN** diagnostics record a cache hit and the public response schema stays unchanged
