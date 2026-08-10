## ADDED Requirements

### Requirement: Firmware revision probes SHALL use a QGMR-first policy

AT modem backends SHALL query `AT+QGMR` before `AT+CGMR` for the firmware revision. They SHALL use the fallback only when QGMR returns a modem error, timeout, or invalid payload, and SHALL return the command source with the normalized revision.

#### Scenario: DJI/Quectel revision is available through QGMR
- **WHEN** `AT+QGMR` returns a valid single revision line
- **THEN** the backend returns that revision with source `AT+QGMR` and does not send `AT+CGMR`

#### Scenario: QGMR is unsupported
- **WHEN** `AT+QGMR` returns `ERROR` and `AT+CGMR` returns a valid revision
- **THEN** the backend returns the CGMR revision with source `AT+CGMR`

#### Scenario: Both revision commands fail
- **WHEN** both commands return errors or invalid payloads
- **THEN** the backend returns an unknown revision with a non-sensitive reason and does not manufacture a version from another field

### Requirement: Firmware revision parsers SHALL reject ambiguous responses

Revision parsing SHALL remove command echo, terminal status lines, quotes, and unrelated URCs; accept `+QGMR:`/`+CGMR:` and unprefixed revision lines; and reject responses containing zero or multiple plausible revision values.

#### Scenario: Response contains echo and terminal status
- **WHEN** a response contains the command echo, one revision line, and `OK`
- **THEN** the parser returns only the revision line

#### Scenario: Response contains an unrelated URC
- **WHEN** a response contains a registration URC and one revision line
- **THEN** the parser ignores the URC and returns the revision

