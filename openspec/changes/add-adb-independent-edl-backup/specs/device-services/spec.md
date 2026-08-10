## ADDED Requirements

### Requirement: Device-control settings SHALL be owned by the application service

The application service SHALL validate and persist ADB and EDL settings as one atomic device-control document. It SHALL not read tool configuration from browser-only storage or from independent firmware/ADB settings namespaces.

#### Scenario: Settings contain one invalid executable
- **WHEN** a device-control settings request contains an invalid ADB or EDL executable
- **THEN** the service rejects the whole document and leaves the previous document unchanged

#### Scenario: Settings are cleared
- **WHEN** a client submits an explicit empty optional path
- **THEN** the service clears that field in the single device-control document and returns the effective availability reason

### Requirement: Device status SHALL use the shared firmware revision probe

The device-control status path and the device identity path SHALL use the same QGMR-first revision probe and parser. A cached revision SHALL be retained across EDL re-enumeration with an explicit cached source.

#### Scenario: Normal mode status is refreshed
- **WHEN** the modem is in a normal AT-capable mode
- **THEN** status and device identity report the same normalized revision and probe source

#### Scenario: EDL status is refreshed
- **WHEN** the modem is in EDL and no AT channel is available
- **THEN** status retains the last known revision only as cached data and reports that no live probe ran

#### Scenario: Device reconnect is in progress after reset
- **WHEN** the normal USB identity has returned but the runtime backend is still connecting or initializing
- **THEN** device and device-control status return the current reconnecting snapshot without a `device is not ready` API error

### Requirement: Device-control service SHALL arbitrate mode-changing backup work

The device-control service SHALL acquire the existing device resource lock before EDL entry, Firehose read, reset, and reconnect. It SHALL pass context cancellation through every platform and child-process port and SHALL use bounded cleanup deadlines for reset recovery.

#### Scenario: A second device operation starts during EDL backup
- **WHEN** an EDL entry or backup operation holds the device resource
- **THEN** another mode-changing firmware operation receives the existing structured operation-conflict error and no second transport is opened

#### Scenario: Shutdown cancels a backup
- **WHEN** application shutdown closes operation admission while a NAND read is running
- **THEN** the child process and transport are cancelled, the cleanup reset is bounded, and the resource lock is released

### Requirement: Device-control status SHALL report tool and platform capability reasons

The device-control application service SHALL report whether direct EDL switching and complete NAND backup are available, together with a reason for each unavailable method. It SHALL not report backup capability when the configured EDL client cannot perform both read and reset with a validated loader.

#### Scenario: EDL client lacks reset support
- **WHEN** the configured tool can read NAND but cannot run the Firehose reset path
- **THEN** firmware status marks complete backup unavailable and explains that reset support is missing

#### Scenario: Direct switch is unavailable on the active platform
- **WHEN** the platform adapter does not implement the DIAG port
- **THEN** firmware status reports ADB as the available entry method only when an online ADB device can be selected
