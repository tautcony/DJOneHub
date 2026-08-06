## ADDED Requirements

### Requirement: The application SHALL shut down in reverse start order through one path

UI-exit and signal-driven shutdown SHALL converge on a single shutdown path. The shutdown path SHALL close application and operation admission before draining HTTP handlers, cancel and join in-flight operations, then stop each background worker in reverse of the actual start order (Notification, Extras, Network, SMS, Runtime) and wait for each to join before closing storage. HTTP draining and worker stopping SHALL use separate bounded contexts. If a worker does not stop by its deadline, shutdown SHALL return an error and SHALL NOT close storage while that worker can still write. It SHALL keep the native UI alive until notification sink calls have returned, SHALL NOT deliver events to a native UI that has already stopped, and SHALL NOT start the native UI when the HTTP server failed to start.

#### Scenario: Signal arrives while the UI is running
- **WHEN** a termination signal arrives while the native UI is running
- **THEN** exactly one shutdown sequence runs: workers stop and join in reverse start order, then storage closes, and no event is delivered to the stopped UI afterwards

#### Scenario: Poller writes during shutdown
- **WHEN** a polling service is mid-refresh while the application shuts down
- **THEN** shutdown waits for the poller to stop before closing the store, so no in-flight write races with the closed database

#### Scenario: Operation is running during shutdown
- **WHEN** shutdown begins while an asynchronous operation is running
- **THEN** new operations are rejected, the running operation is cancelled and joined before storage closes, and its terminal state is `cancelled`

#### Scenario: HTTP server fails to start
- **WHEN** the HTTP server fails to listen
- **THEN** the application exits through the normal shutdown path and the native UI is not started against an unreachable URL
