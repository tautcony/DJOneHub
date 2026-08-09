## MODIFIED Requirements

### Requirement: UI SHALL render asynchronous operations and events
The Runtime view SHALL render a dedicated event-source list from runtime diagnostics, including source state, polling interval, and emitted event types, alongside the existing topology and message traces. It SHALL distinguish these sources from transport and cleanup mechanisms.

#### Scenario: Event-source list is shown
- **WHEN** runtime diagnostics are available
- **THEN** the page lists each event-producing worker and its interval and event families

#### Scenario: Event-source state changes
- **WHEN** a source becomes stopped or degraded
- **THEN** its displayed state updates on the next diagnostics refresh without changing the topology contract

#### Scenario: No event sources are available
- **WHEN** diagnostics are unavailable or contain no event sources
- **THEN** the page shows the existing unavailable/empty state without rendering mechanism timers as sources

#### Scenario: eSIM progress arrives
- **WHEN** an eSIM operation is accepted by REST and progress events arrive over WebSocket
- **THEN** the UI shows the operation state and final result using the same operation identity
