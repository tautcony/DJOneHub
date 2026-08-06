## ADDED Requirements

### Requirement: VoWiFi lifecycle transitions SHALL be serialized and converge

Enable, disable, reconnect, and recovery operations SHALL be serialized through one state owner so concurrent transitions cannot interleave on the same port. Every failure path SHALL cancel the session context and close any port it opened, the runtime-event subscription SHALL be tied to the session lifecycle, and event-driven recovery SHALL be single-flight and debounced so a flapping network cannot spawn unbounded concurrent recovery attempts.

#### Scenario: Enable fails after the port was opened
- **WHEN** enabling VoWiFi fails after the port was opened
- **THEN** the failure path cancels the session context and closes the port, and a retry starts from a clean state without leaking ports or event consumers

#### Scenario: Network flaps during recovery
- **WHEN** repeated network or modem events trigger recovery within a short window
- **THEN** the host runs one debounced recovery at a time instead of spawning concurrent recovery goroutines
