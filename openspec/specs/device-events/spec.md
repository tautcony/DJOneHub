## Purpose

Define the event stream contract used to publish device state, operation progress, and terminal results to connected clients.
## Requirements
### Requirement: WebSocket events SHALL use a versioned envelope

Every event SHALL include an `id`, `type`, `version`, `occurred_at`, and `data` field, and SHALL represent device, backend, SMS, eSIM, network, or VoWiFi changes from the runtime event source.

#### Scenario: Status changes
- **WHEN** a device transitions from `connecting` to `ready`
- **THEN** connected clients receive a `device.status.changed` event with the new state and backend data

### Requirement: A new WebSocket session SHALL receive a snapshot first

After the loopback Origin/Host check (accepting `127.0.0.1`, `localhost`, or `::1` on the bound port) and connection — with login authentication deferred and not a prerequisite for this local boundary — the event endpoint SHALL use an EventBus subscribe-with-watermark operation before building the current single-device snapshot. That operation SHALL return the subscription, its captured sequence, an observable drop counter, and an idempotent unsubscribe function while holding the bus lock. The endpoint SHALL assign the snapshot that watermark ID, send the snapshot first, and then deliver every queued event with an ID greater than the watermark. The snapshot contract covers device status only; operation, SMS, and call events SHALL NOT be discarded as snapshot-covered. If the subscription overflows, the endpoint SHALL close the session so the client obtains a fresh snapshot instead of continuing with an unknown event gap.

#### Scenario: Malicious page opens the event socket
- **WHEN** a page with a disallowed Origin opens the event WebSocket
- **THEN** the upgrade is rejected and no event stream is established

#### Scenario: Same-origin loopback client reconnects
- **WHEN** a same-origin loopback client reconnects with an allowed Origin and Host after a network interruption
- **THEN** the session is subscribed before the snapshot is sent, events published during snapshot construction are delivered after it, and the client can discard stale local state before applying new events

#### Scenario: Client deduplicates by event ID
- **WHEN** a client applies the snapshot and then applies subsequent incremental events
- **THEN** the snapshot ID and the incremental event IDs are monotonic so client-side deduplication never discards a delivered event

### Requirement: Long-operation progress SHALL be broadcast consistently

The event system SHALL publish progress and terminal events for SMS, eSIM, network, AT, backend, and VoWiFi operations using the same operation identity returned by the REST API.

#### Scenario: Operation fails after progress
- **WHEN** an eSIM operation reports progress and then encounters a retryable failure
- **THEN** clients receive progress followed by a terminal event containing the operation identity and structured retryability information

### Requirement: WebSocket upgrades SHALL validate origin and host

The event endpoint SHALL validate the Origin and Host headers during WebSocket upgrade against a loopback origin (any of `http://127.0.0.1:<port>`, `http://localhost:<port>`, or `http://[::1]:<port>` for the bound port) and SHALL reject upgrades that fail either check. Login authentication is deferred and is not part of this contract.

#### Scenario: Malicious page opens the event socket
- **WHEN** a page with a disallowed Origin opens the event WebSocket
- **THEN** the upgrade is rejected and no event stream is established

#### Scenario: Same-origin client reconnects
- **WHEN** a same-origin loopback client reconnects with an allowed Origin and Host
- **THEN** the upgrade succeeds and the session receives a snapshot before incremental events

### Requirement: Event publishing SHALL be non-blocking and account for drops

The event bus SHALL publish events to all subscribers without ever blocking the publishing call, SHALL count events dropped for a slow subscriber, and SHALL expose cumulative and active-subscriber drop counts through the existing notification-debug diagnostics response so silent loss is diagnosable. Unsubscribing SHALL remove active-subscriber diagnostic state. Runtime diagnostics SHALL identify every periodic worker that produces domain events. The diagnostics SHALL include the worker interval and event families.

#### Scenario: Event sources are observable
- **WHEN** the runtime diagnostics endpoint is queried
- **THEN** device discovery, backend event consumption, SMS refresh, network refresh, traffic sampling, and call monitoring are marked as event sources with their configured interval where applicable and emitted event types

#### Scenario: Mechanism timers are excluded
- **WHEN** transport keepalive, SSE flush, cleanup, retry, or UI refresh mechanisms run
- **THEN** they are not reported as periodic domain-event sources

#### Scenario: Slow subscriber
- **WHEN** a subscriber's buffer is full while a new event is published
- **THEN** the publisher does not block and the drop counter for that subscriber increments

#### Scenario: Drop accounting is observable
- **WHEN** events have been dropped for any subscriber
- **THEN** the cumulative and active-subscriber drop counts are available in `GET /api/v1/notifications/debug` instead of being silently discarded

#### Scenario: Subscriber disconnects after drops
- **WHEN** a subscriber with a non-zero drop count unsubscribes
- **THEN** its entry is removed from active-subscriber diagnostics while the cumulative count remains monotonic

### Requirement: WebSocket sessions SHALL be reclaimed when stale

The event endpoint SHALL continuously read from each connected WebSocket session, SHALL enforce read and write deadlines, SHALL respond to keepalive pings, and SHALL terminate sessions that do not honor the keepalive so slow or silent clients cannot hold goroutines and event subscriptions indefinitely.

#### Scenario: Silent client
- **WHEN** a client stops reading or sending frames beyond the enforced deadlines
- **THEN** the server closes the session and releases its goroutine and event subscription

#### Scenario: Healthy client
- **WHEN** a client responds to keepalive pings within the deadline
- **THEN** the session remains open and continues receiving events

### Requirement: Event payloads SHALL use typed sanitizers and an evidence-based field blacklist

The public event boundary covers WebSocket events and REST status or snapshot payloads. The boundary SHALL use explicit typed projections for known sensitive payload types. It SHALL use a recursive, case-insensitive field blacklist for raw map payloads. Each blacklist entry SHALL identify a current event or operation producer that can put sensitive data in that field. A raw map field that is not in the blacklist SHALL remain present by default.

The projections SHALL preserve the existing outer shape of `domain.Snapshot` and `device.Status`. Device identity values SHALL remain present in `device.status.changed`, WebSocket snapshot, and REST `device.Status` payloads because the web Overview view masks these values and the loopback boundary restricts access. The sanitizer SHALL replace raw error and reason text with stable fallback text. REST data endpoints, including SMS lists and call history, remain outside this sanitizer.

An `operation.log` event SHALL preserve the exact terminal message. The sanitizer SHALL not remove ANSI sequences, carriage returns, newlines, whitespace-only chunks, or other process-output bytes from that message. The sanitizer SHALL not use content heuristics such as replacing text that contains CJK characters.

#### Scenario: Raw event contains a blacklisted field
- **WHEN** a raw event map contains a field whose current producer can expose an SMS body, phone number, card identifier, hardware serial, local path, raw protocol buffer, or backend error detail
- **THEN** the sanitizer removes that field recursively and preserves non-blacklisted sibling fields

#### Scenario: Raw event contains an unlisted field
- **WHEN** a raw event map contains a new field that has no documented sensitive producer
- **THEN** the sanitizer preserves the field instead of requiring it to be added to an allowlist

#### Scenario: Known SMS or call event is published
- **WHEN** an SMS or call event uses its typed public payload
- **THEN** the typed projection removes the SMS content or call number according to that event contract without adding every typed field name to the raw-map blacklist

#### Scenario: Operation emits terminal output
- **WHEN** a NAND operation publishes stdout or stderr that contains ANSI sequences, carriage returns, newlines, or whitespace-only chunks
- **THEN** the public `operation.log` event preserves the message unchanged for xterm rendering

#### Scenario: Raw backend event contains a disallowed field
- **WHEN** a raw backend event reaches the public event stream carrying a blacklisted field
- **THEN** the blacklisted field is removed while non-blacklisted sibling fields are preserved

#### Scenario: SMS body is not allowlisted
- **WHEN** an SMS received event is published through its typed public projection
- **THEN** the message body is removed without applying an allowlist to unrelated raw-map fields

#### Scenario: Unknown event fields are present
- **WHEN** an event contains a raw-map field that is not in the evidence-based blacklist
- **THEN** the field remains present in both HTTP and WebSocket output

#### Scenario: Device status is sanitized
- **WHEN** a REST status or WebSocket snapshot payload passes through the typed sanitizer
- **THEN** the `snapshot`, `identity`, `radio`, and `sim` object structure remains present with device identity values intact, while error and reason text uses stable fallback text

### Requirement: EDL session changes SHALL be ordered events

The event bus SHALL publish EDL observation, lease, and recovery state changes with the existing monotonic event ID. A new WebSocket snapshot SHALL include the current EDL session state before incremental events.

The event type SHALL be `device_control.edl_session_changed`. Its data SHALL use the public session schema. The data SHALL omit the lease token and physical location. The initial `snapshot` event SHALL retain the existing device status fields and SHALL add optional `edl_session` at the top level.

#### Scenario: A lease changes owner state
- **WHEN** a lease is acquired, renewed, released, or expires
- **THEN** connected clients receive an ordered session event after the initial snapshot

