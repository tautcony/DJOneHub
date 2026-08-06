## ADDED Requirements

### Requirement: Device and user data SHALL stay out of default log output

The device-layer logger SHALL NOT write sensitive identity or content data at default (Info) level: IMEI and ICCID SHALL be masked or logged only at Debug level, SMS message content, USSD text, and dialed or incoming call numbers SHALL NOT be logged by default (only when an explicit switch enables it), and eSIM profile download logs SHALL omit the `matchingID` activation-code component.

#### Scenario: Device identity is logged
- **WHEN** a modem log statement at Info level reports device identity
- **THEN** the IMEI and ICCID appear masked, with full values only at Debug level or under an explicit switch

#### Scenario: SMS content would be logged
- **WHEN** the modem or SMS path logs message processing while the content-logging switch is off
- **THEN** message bodies, USSD text, and call numbers are absent from the default log output

#### Scenario: eSIM profile download is logged
- **WHEN** the eSIM download path logs progress or failure
- **THEN** the log omits the `matchingID` so the one-time activation code component is not persisted
