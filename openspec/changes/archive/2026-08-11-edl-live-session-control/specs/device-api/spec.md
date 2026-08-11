## ADDED Requirements

### Requirement: Device-control API SHALL expose EDL session state and lease operations

The device-control status response SHALL include the current EDL observation and session ownership state. The API SHALL provide bounded lease acquire, renew, and release actions. Mutating actions SHALL require the current lease and SHALL return `device_session_conflict` when another client owns it.

The API SHALL use `/api/v1/device-control/session/lease` for lease actions. `POST` SHALL acquire a lease and return HTTP 201 with `lease_token` and `session`. `PUT` SHALL renew a lease. `DELETE` SHALL release a lease. `PUT`, `DELETE`, and each device-control mutation SHALL read the opaque token from `X-DJOneHub-Device-Lease`.

The browser WebSocket API cannot set a custom header. The ADB Shell WebSocket SHALL carry the same token in the `djonehub-device-lease.<token>` WebSocket subprotocol. The server SHALL validate the token before it opens the device shell. The server SHALL pin the lease until the WebSocket closes.

The `session` object SHALL contain `session_id`, `observation`, `lease_held`, `lease_owned`, optional `lease_expires_at`, and optional `active_operation`. The public API SHALL omit `physical_location`. The public API SHALL mask `serial_number`, `hardware_id`, and `pk_hash`.

The `device_session_conflict` error SHALL use HTTP 409. Its public details MAY contain `session_id`. Its public details SHALL NOT contain the lease token or physical location.

#### Scenario: Another client owns the lease
- **WHEN** a client submits a mutating action with no current lease token
- **THEN** the API returns `device_session_conflict` and starts no operation

#### Scenario: The owner reads status
- **WHEN** a client supplies the current token on `GET /api/v1/device-control`
- **THEN** `edl_session.lease_owned` is true for that response only
