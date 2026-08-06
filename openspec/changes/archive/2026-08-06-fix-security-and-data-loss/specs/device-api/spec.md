## MODIFIED Requirements

### Requirement: API handlers SHALL enforce the local authentication boundary

Until the first-use authentication flow is specified, the process SHALL reject any listen address whose host is not a loopback form (`127.0.0.1`, `localhost`, or `::1`). State-changing API handlers SHALL validate Origin/Host and SHALL reject cross-site simple writes before invoking application services. No credential check is required by this temporary boundary; login authentication remains deferred.

#### Scenario: Non-loopback server is requested
- **WHEN** the server is configured with a wildcard, non-loopback, or hostname listen address
- **THEN** startup fails before the application or native UI begins serving

#### Scenario: Loopback server is requested
- **WHEN** the server is configured with `127.0.0.1`, `localhost`, or `[::1]`
- **THEN** startup proceeds and the temporary loopback boundary applies

#### Scenario: Cross-site simple request reaches a write endpoint
- **WHEN** a cross-site request submits a write operation with a non-JSON content type or a disallowed origin
- **THEN** the API rejects the request before invoking the application service

#### Scenario: Same-origin write is submitted
- **WHEN** a client submits a state-changing request from an allowed loopback origin
- **THEN** the API accepts it subject to normal method, content-type, and DTO validation

#### Scenario: Unauthenticated command is submitted
- **WHEN** a same-origin loopback client submits a protected device command without login credentials
- **THEN** the temporary boundary does not reject it for missing credentials and still applies Origin/Host, method, content-type, DTO, and capability validation

## ADDED Requirements

### Requirement: OpenAPI declarations SHALL match the temporary API boundary

The OpenAPI document SHALL not declare login credentials or a security scheme that the server does not enforce.

#### Scenario: Security scheme is inspected
- **WHEN** a client reads the OpenAPI document
- **THEN** it contains no deferred credential requirement
