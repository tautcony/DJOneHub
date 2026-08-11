## ADDED Requirements

### Requirement: Platform AT adapters SHALL expose physical transport operations only

A platform AT adapter SHALL own device discovery, physical identity selection, physical transport opening, physical transport closing, and platform-specific recovery. It SHALL expose the opened transport through the shared AT transport contract and SHALL NOT construct a platform-specific AT business backend.

#### Scenario: macOS selects a USB AT interface

- **WHEN** the macOS adapter finds the allow-listed interface and endpoints for the selected physical device
- **THEN** it returns an opened byte transport to the shared AT factory and does not construct a command backend

#### Scenario: Linux or Windows selects a serial AT port

- **WHEN** the Linux or Windows adapter finds a responsive serial port
- **THEN** it records the port on the candidate and the shared AT factory opens it for the common command session

#### Scenario: Platform transport is unavailable

- **WHEN** the selected platform cannot open its verified AT transport
- **THEN** backend creation fails without publishing AT capabilities or fabricating a connected device
