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

The event bus SHALL publish events to all subscribers without ever blocking the publishing call, SHALL count events dropped for a slow subscriber, and SHALL expose cumulative and active-subscriber drop counts through the existing notification-debug diagnostics response so silent loss is diagnosable. Unsubscribing SHALL remove active-subscriber diagnostic state.

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

### Requirement: Event payloads SHALL be sanitized by an explicit field allowlist

The event endpoint SHALL sanitize every event and snapshot payload of the public event stream — WebSocket events and REST status/snapshot payloads — through explicit typed projections for known payloads and an explicit field allowlist for raw maps. The projections SHALL preserve the existing outer shape of `domain.Snapshot` and `device.Status`; fields not on the allowlist SHALL be redacted in raw backend events, SMS received bodies, and status payload error/reason text. Device identity (IMEI, ICCID, IMSI, EID) SHALL remain present in `device.status.changed` and `snapshot`/REST `device.Status` payloads, because the web Overview card renders it (client-side masked) and the loopback boundary protects it; only raw error/reason text is replaced with fallback text. REST data endpoints (SMS list, call history) are outside this sanitizer's scope. The sanitizer SHALL NOT rely on content heuristics such as replacing text containing CJK characters.

#### Scenario: Raw backend event contains a disallowed field
- **WHEN** a raw backend event reaches the public event stream carrying fields outside the allowlist
- **THEN** the disallowed fields are redacted while the allowlisted fields are passed through

#### Scenario: SMS body is not allowlisted
- **WHEN** an SMS received event is published and its message body is not on the public allowlist
- **THEN** the body is redacted consistently instead of being passed through and only re-detected by a content heuristic

#### Scenario: Unknown event fields are present
- **WHEN** a call, operation, status, or unknown event contains a field outside its event-family allowlist
- **THEN** the field is redacted in both HTTP and WebSocket output, including when the payload is a raw `map[string]any`

#### Scenario: Device status is sanitized
- **WHEN** a REST status or WebSocket snapshot payload is projected through the allowlist
- **THEN** the `snapshot`, `identity`, `radio`, and `sim` object structure remains present with device identity values intact, while error and reason text is replaced with fallback text

