## ADDED Requirements

### Requirement: AT command timing SHALL distinguish queueing from execution

The shared AT command session SHALL record bounded structured timing for each
completed command. The record SHALL include queue wait duration, execution
duration, terminal result, and timeout class. It SHALL NOT contain command
payloads, APDU data, responses, activation data, or unmasked identifiers.

#### Scenario: Command waits behind eSIM work

- **WHEN** a status AT command waits in the Manager queue before execution
- **THEN** its diagnostic record reports `queue_wait_ms` separately from `exec_ms`

#### Scenario: Command contains sensitive data

- **WHEN** an AT command contains an APDU, phone number, or credential
- **THEN** the timing record identifies only a safe command class and does not log the command or response content
