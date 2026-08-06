## ADDED Requirements

### Requirement: Backend event delivery SHALL NOT block command processing

AT and QMI backend event channels SHALL deliver events to subscribers without ever blocking the backend's command loop or event dispatch, SHALL count events dropped for a slow subscriber, and SHALL expose the drop counts so silent loss is diagnosable.

#### Scenario: Slow event consumer on AT
- **WHEN** the AT backend's event channel is full while a new event is published
- **THEN** the send does not block the AT command loop and the dropped event is counted

#### Scenario: QMI event dispatch continues
- **WHEN** the QMI backend's event channel is full
- **THEN** event dispatch continues without stalling and dropped events are counted
