## ADDED Requirements

### Requirement: The API SHALL provide versioned single-device resources

The HTTP API SHALL expose `/api/v1` resources for device identity, capabilities, status, rescan, raw AT, SMS, eSIM, network, and VoWiFi, plus the `/api/v1/events/ws` event endpoint.

#### Scenario: Client reads device state
- **WHEN** a client requests `GET /api/v1/device/status`
- **THEN** the API returns the current single-device state, backend, capability-derived status, and a stable response schema

#### Scenario: Client requests rescan
- **WHEN** a client posts to `/api/v1/device/actions/rescan`
- **THEN** the API validates the request, invokes the application service, and returns the operation or result without opening a transport in the handler

### Requirement: API errors SHALL use one structured format

Failed API calls SHALL return an error object containing `code`, `message`, `retryable`, and optional `details`; unsupported capabilities SHALL use `capability_not_supported`.

#### Scenario: Unsupported operation is submitted
- **WHEN** a client requests an operation absent from the current capability set
- **THEN** the API returns the structured error with HTTP status appropriate to the failure and identifies the missing capability

### Requirement: API handlers SHALL enforce the local authentication boundary

The API SHALL apply the configured local authentication boundary before invoking application services and SHALL keep transport, DTO, and validation concerns outside domain code.

#### Scenario: Unauthenticated command is submitted
- **WHEN** a client submits a protected device command without satisfying configured local authentication
- **THEN** the API rejects the request and does not invoke the application service
