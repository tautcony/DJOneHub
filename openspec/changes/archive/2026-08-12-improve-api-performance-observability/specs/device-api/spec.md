## ADDED Requirements

### Requirement: API routes SHALL have canonical workload metadata

Each registered API method SHALL define its HTTP method, canonical path
template, workload class, stream kind, handler, and OpenAPI operation in one
typed route registry. The registry SHALL drive dispatch, OpenAPI generation,
request deadline selection, and route performance labels.

#### Scenario: OpenAPI is generated
- **WHEN** the server builds the OpenAPI document
- **THEN** every registered method is represented from the route registry

#### Scenario: Prefix route completes
- **WHEN** a path contains a concrete identifier
- **THEN** performance diagnostics use the registered path template

### Requirement: HTTP deadlines SHALL use workload policy

Non-stream routes SHALL receive a deadline from one centralized workload policy
table. Stream routes SHALL keep their existing keepalive and write deadlines and
SHALL NOT receive a normal request deadline.

#### Scenario: Stream remains healthy
- **WHEN** a WebSocket or Server-Sent Events client satisfies its keepalive contract
- **THEN** the connection remains active beyond normal request deadlines

### Requirement: Route policy changes SHALL preserve public API schemas

Route metadata, deadlines, metrics, and logging changes SHALL preserve existing
`/api/v1` success and structured-error schemas.

#### Scenario: Existing client reads status
- **WHEN** a client reads a status route after registry migration
- **THEN** it receives the existing response shape and capability behavior
