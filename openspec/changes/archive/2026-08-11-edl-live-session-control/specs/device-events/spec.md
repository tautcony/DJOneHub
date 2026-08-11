## ADDED Requirements

### Requirement: EDL session changes SHALL be ordered events

The event bus SHALL publish EDL observation, lease, and recovery state changes with the existing monotonic event ID. A new WebSocket snapshot SHALL include the current EDL session state before incremental events.

The event type SHALL be `device_control.edl_session_changed`. Its data SHALL use the public session schema. The data SHALL omit the lease token and physical location. The initial `snapshot` event SHALL retain the existing device status fields and SHALL add optional `edl_session` at the top level.

#### Scenario: A lease changes owner state
- **WHEN** a lease is acquired, renewed, released, or expires
- **THEN** connected clients receive an ordered session event after the initial snapshot
