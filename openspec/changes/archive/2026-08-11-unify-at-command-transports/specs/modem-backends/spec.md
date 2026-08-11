## ADDED Requirements

### Requirement: All AT transports SHALL use one command session

Every AT transport SHALL use the shared command session for command serialization, terminal response classification, unsolicited result code separation, interactive prompts, bounded response collection, timeout quarantine, and transport recovery. A platform-specific transport SHALL NOT implement a separate AT command state machine.

#### Scenario: Linux or Windows serial AT device connects

- **WHEN** a platform adapter supplies an operating-system serial port
- **THEN** the AT factory starts the shared command session on that serial transport

#### Scenario: macOS USB AT device connects

- **WHEN** the macOS adapter supplies an opened USB bulk AT transport
- **THEN** the AT factory starts the same shared command session on that USB transport

#### Scenario: Device returns a terminal error

- **WHEN** any AT transport returns `ERROR`, `+CME ERROR`, or `+CMS ERROR` for the active command
- **THEN** the shared command session returns a command error and does not report the response as success

#### Scenario: Command response arrives after timeout

- **WHEN** an AT command times out on a serial or USB transport
- **THEN** the shared command session isolates the late response before it accepts another command

### Requirement: AT modem behavior SHALL be implemented once

Identity, SIM, radio, Short Message Service, raw AT, network control, Unstructured Supplementary Service Data, SIM authentication, eSIM delegation, and modem event behavior for AT devices SHALL be provided by one AT backend implementation. Platform adapters SHALL NOT duplicate these modem operations.

#### Scenario: The same modem command runs on different platforms

- **WHEN** macOS, Linux, and Windows request the same supported AT modem operation
- **THEN** each platform uses the same backend method, command construction, parser, error behavior, and APDU arbitration rule

#### Scenario: eSIM and SIM authentication share the transport

- **WHEN** eSIM work and SIM authentication use the same AT device
- **THEN** both paths use the same device-level APDU arbiter and the same shared command session
