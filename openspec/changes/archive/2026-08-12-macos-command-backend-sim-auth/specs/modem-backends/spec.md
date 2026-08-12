## ADDED Requirements

### Requirement: CommandBackend SHALL provide AT SIM authentication

The raw AT `CommandBackend` SHALL implement `SIMAuthProvider` with AT+CCHO, AT+CGLA, and AT+CCHC. It SHALL validate logical channel bounds and reject malformed APDU responses.

#### Scenario: Open and transmit an AKA channel

- **WHEN** the transport returns `+CCHO: 2` and a valid `+CGLA` response
- **THEN** the backend returns channel 2 and the APDU response payload

#### Scenario: Malformed channel response

- **WHEN** AT+CCHO does not return a positive channel from 1 through 255
- **THEN** the backend returns an error and does not claim APDU success

### Requirement: CommandBackend SHALL report APDU capability

The backend capability set SHALL include `apdu` when its AT transport is present, so the business adapter exposes VoWiFi SIM authentication on macOS USB.

#### Scenario: USB AT transport is present

- **WHEN** a CommandBackend is created with an AT transport
- **THEN** its capability set includes `apdu`

### Requirement: AT SIM authentication SHALL share the device APDU arbiter

When an arbiter is injected, each CommandBackend SIMAuth command SHALL acquire and release a `USIMAKA` transport lease. The macOS adapter SHALL inject the same arbiter instance into the eSIM AT port and CommandBackend.

#### Scenario: VoWiFi and eSIM share APDU arbitration

- **WHEN** the macOS adapter opens a USB AT backend
- **THEN** the CommandBackend and eSIM AT port receive the same device-level arbiter

### Requirement: VoWiFi SHALL accept the direct CommandBackend

The VoWiFi host SHALL obtain the identity, serving-system, operating-mode, and SIM authentication surfaces from a direct CommandBackend without requiring the incompatible legacy `DeviceBackend` SMS contract.

#### Scenario: macOS USB backend starts VoWiFi preparation

- **WHEN** the runtime backend is a CommandBackend that advertises `apdu`
- **THEN** VoWiFi preparation accepts the backend and constructs a logical-channel modem adapter

### Requirement: VoWiFi startup failures SHALL remain visible to the client

The runtime SHALL copy a terminal startup error into the public VoWiFi state. The VoWiFi API SHALL expose the error in `last_error` and the operation status SHALL expose its structured error message.

#### Scenario: SWU tunnel startup fails

- **WHEN** tunnel establishment returns a protocol error
- **THEN** the VoWiFi state becomes `failed`, `/api/v1/vowifi` includes `last_error`, and the operation view displays the error message
