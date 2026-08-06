## ADDED Requirements

### Requirement: SIM identity files SHALL decode consistently across backends

The AT and MBIM paths SHALL decode EF_IMSI to the same full IMSI value: the AT path SHALL NOT truncate the first digit under the parity-bit assumption, and both paths SHALL be pinned by unit tests against the standard 3GPP TS 31.102 layout so the MCC is preserved.

#### Scenario: AT path reads a standard EF_IMSI
- **WHEN** the AT path decodes an EF_IMSI stored in the standard layout
- **THEN** it returns the full IMSI with the MCC intact instead of dropping the first digit

#### Scenario: AT and MBIM paths read the same SIM
- **WHEN** both backends decode EF_IMSI from the same SIM
- **THEN** they return identical IMSI values

### Requirement: Status polling SHALL NOT modify operator format selection

Polling queries SHALL issue only `AT+COPS?` and SHALL parse the format reported by the modem. A polling query SHALL never issue `AT+COPS=3,2`, infer a replacement format, or otherwise modify modem state. An explicit operator-format command is the only path allowed to change the format; a query parse failure SHALL be returned as a classified error.

#### Scenario: Serving-cell polls run repeatedly
- **WHEN** the polling path reads serving-cell or operator state repeatedly
- **THEN** the user's operator format selection is not rewritten by the polling itself

#### Scenario: Format cannot be parsed
- **WHEN** the `AT+COPS?` response does not contain a supported format
- **THEN** polling returns a classified parse error and does not issue any format-setting command

### Requirement: All APDU transports SHALL be coordinated by the device-level arbiter

Every APDU-capable transport, including the pure-AT eSIM port, SHALL share the device-level APDU arbiter instance so SIM-switch barriers and APDU-idle waits apply on all paths and no transport bypasses arbitration.

#### Scenario: Pure-AT eSIM port is used
- **WHEN** the darwin pure-AT eSIM port performs a SIM switch while another component uses the device APDU channel
- **THEN** the switch is coordinated through the same device-level arbiter as the modem path, so the barrier and APDU-idle waits are enforced instead of being no-ops

#### Scenario: VoWiFi AKA auth overlaps an eSIM operation
- **WHEN** VoWiFi AKA authentication and an eSIM APDU operation overlap on the pure-AT path
- **THEN** both are serialized by the shared arbiter and the conflict window is eliminated

### Requirement: AT-channel APDU transport SHALL be implemented once

The near-identical AT APDU channel wrappers SHALL be consolidated into one shared implementation used by all AT-channel APDU consumers, with uniform behavior including rejecting transmissions on channel zero and precompiled command patterns.

#### Scenario: Transmit on channel zero
- **WHEN** an AT APDU channel is asked to transmit on the basic channel (channel zero)
- **THEN** the transmission is rejected uniformly by the shared implementation

#### Scenario: APDU-heavy profile download
- **WHEN** a profile download transmits hundreds of APDUs through the AT channel
- **THEN** command patterns are compiled once at package load, not recompiled per APDU
