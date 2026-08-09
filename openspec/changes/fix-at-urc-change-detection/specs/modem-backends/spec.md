## ADDED Requirements

### Requirement: AT status change handling SHALL distinguish responses from transitions

The AT command layer SHALL retain a status line that belongs to the active query in that command's response and SHALL NOT log, publish, or dispatch it as an unsolicited state change. For genuine unsolicited registration and SIM status reports, the modem manager SHALL compare the parsed value with the last successfully observed value and SHALL perform change handling only when the value is initially unknown or has actually changed.

#### Scenario: Polling receives a stable registration response
- **WHEN** `AT+CEREG?` is active and the modem returns a `+CEREG` response containing the same registration state as the previous poll
- **THEN** the response is returned to the query parser and no registration-change URC is logged or dispatched

#### Scenario: Polling receives a stable SIM response
- **WHEN** `AT+QSIMSTAT?` or `AT+CPIN?` is active and the modem returns its corresponding status response
- **THEN** the response remains part of the command result and does not trigger SIM-change callbacks or modem-reset handling

#### Scenario: Modem repeats an unsolicited state value
- **WHEN** the modem emits a registration or SIM URC whose parsed value matches the last successfully observed value
- **THEN** the manager suppresses change logging, callbacks, and follow-up change handling

#### Scenario: Modem reports a real unsolicited transition
- **WHEN** the modem emits a valid registration or SIM URC whose parsed value differs from the last successfully observed value
- **THEN** the manager updates its baseline and performs the existing state-change logging and dispatch exactly once
