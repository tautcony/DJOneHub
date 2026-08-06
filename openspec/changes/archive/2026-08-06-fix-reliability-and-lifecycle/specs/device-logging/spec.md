## ADDED Requirements

### Requirement: Structured logging SHALL be initialized at application startup

The application SHALL initialize the structured logger at startup before device services begin, so log output from the modem, backend, and eSIM layers is emitted to the configured output instead of being discarded by the default Nop logger.

#### Scenario: Application starts with a device
- **WHEN** the application starts with a device connected
- **THEN** device-layer log statements from the modem, backend, and eSIM layers are emitted to the configured log output
