## ADDED Requirements

### Requirement: Long-running operations SHALL be cancellable at shutdown

The operation manager SHALL reject new work once shutdown admission is closed, SHALL cancel all in-flight operations, SHALL mark each cancelled operation with the cancelled terminal state, and SHALL release the resource locks the cancelled operations held. Starting work after admission closes SHALL return an explicit shutdown/unavailable error rather than an empty operation identifier. Device read paths SHALL honor context cancellation so a cancelled or disconnected request does not continue occupying device resources.

#### Scenario: Shutdown during a long operation
- **WHEN** the application shuts down while a firmware flash or backup operation is running
- **THEN** the operation is cancelled, reports the cancelled terminal state, and releases its resource locks so shutdown completes

#### Scenario: Operation starts after shutdown admission closes
- **WHEN** an API handler attempts to start an operation after shutdown admission has closed
- **THEN** the operation manager returns the structured shutdown/unavailable error, returns no operation ID, and launches no goroutine

#### Scenario: Client disconnects during an eSIM scan
- **WHEN** a client disconnects while an eSIM overview scan is running
- **THEN** the read path observes the cancelled context and stops promptly instead of completing a full profile scan
