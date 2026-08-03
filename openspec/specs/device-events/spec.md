## Purpose

Define the event stream contract used to publish device state, operation progress, and terminal results to connected clients.

## Requirements

### Requirement: WebSocket events SHALL use a versioned envelope

Every event SHALL include an `id`, `type`, `version`, `occurred_at`, and `data` field, and SHALL represent device, backend, SMS, eSIM, network, or VoWiFi changes from the runtime event source.

#### Scenario: Status changes
- **WHEN** a device transitions from `connecting` to `ready`
- **THEN** connected clients receive a `device.status.changed` event with the new state and backend data

### Requirement: A new WebSocket session SHALL receive a snapshot first

After authentication and connection, the event endpoint SHALL send the current single-device snapshot before sending incremental events.

#### Scenario: Frontend reconnects
- **WHEN** the frontend reconnects after a network interruption
- **THEN** it receives a fresh snapshot and can discard stale local state before applying new events

### Requirement: Long-operation progress SHALL be broadcast consistently

The event system SHALL publish progress and terminal events for SMS, eSIM, network, AT, backend, and VoWiFi operations using the same operation identity returned by the REST API.

#### Scenario: Operation fails after progress
- **WHEN** an eSIM operation reports progress and then encounters a retryable failure
- **THEN** clients receive progress followed by a terminal event containing the operation identity and structured retryability information
