## MODIFIED Requirements

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

## ADDED Requirements

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
