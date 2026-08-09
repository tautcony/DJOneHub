## MODIFIED Requirements

### Requirement: Event publishing SHALL be non-blocking and account for drops
The event bus SHALL publish events to all subscribers without ever blocking the publishing call, SHALL count events dropped for a slow subscriber, SHALL expose cumulative and active-subscriber drop counts through diagnostics, and the runtime diagnostics response SHALL identify every periodic worker that produces domain events with its interval and event families.

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
- **THEN** the cumulative and active-subscriber drop counts are available in diagnostics instead of being silently discarded

#### Scenario: Subscriber disconnects after drops
- **WHEN** a subscriber with a non-zero drop count unsubscribes
- **THEN** its entry is removed from active-subscriber diagnostics while the cumulative count remains monotonic
