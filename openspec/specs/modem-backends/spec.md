## Purpose

Define the common modem backend contract and truthful capability behavior across AT, QMI, and MBIM implementations.

## Requirements

### Requirement: Modem backends SHALL expose a common business capability contract

AT, QMI, and MBIM backends SHALL expose identity, radio, SIM, SMS, USSD, APDU, capability discovery, event subscription, timeout, and close semantics where supported.

#### Scenario: Backend is selected
- **WHEN** device configuration and interface probing identify a usable AT, QMI, or MBIM implementation
- **THEN** the runtime selects that backend, records the mode and reason, and exposes its capability set

### Requirement: Backend-specific support SHALL be explicit

Each backend SHALL report supported operations as capabilities, and an unsupported operation SHALL return a standard non-success result with code `capability_not_supported` rather than a fabricated result.

#### Scenario: QMI has no raw AT
- **WHEN** a client submits a raw AT command while the selected backend has no `raw_at` capability
- **THEN** the operation is rejected with `capability_not_supported` and identifies `raw_at` in its details

### Requirement: QMI and MBIM startup SHALL NOT require an AT port

The system SHALL initialize QMI or MBIM data and control capabilities when their own control device is available, even when no AT serial port is present.

#### Scenario: MBIM-only device starts
- **WHEN** a device exposes a usable MBIM control device and no AT port
- **THEN** the runtime can initialize MBIM and reports MBIM capabilities without failing on the missing AT port
