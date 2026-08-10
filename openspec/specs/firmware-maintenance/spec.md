# firmware-maintenance Specification

## Purpose
TBD - created by archiving change add-adb-independent-edl-backup. Update Purpose after archive.
## Requirements
### Requirement: Firmware EDL entry SHALL use a verified method

The firmware service SHALL select a direct DIAG USB switch when the `firmware_edl_switch` capability is present. It SHALL use ADB reboot only when the caller explicitly selects the ADB method or the direct capability is unavailable. A direct protocol or device error SHALL not silently switch to a different ADB device.

#### Scenario: Direct DIAG entry is available
- **WHEN** a client starts the EDL action without an entry method and the selected device advertises `firmware_edl_switch`
- **THEN** the service sends the DIAG reboot frame through the platform port, returns an operation ID, and does not require an ADB serial

#### Scenario: Direct DIAG entry is unavailable
- **WHEN** a client starts the EDL action without an entry method and the selected device does not advertise `firmware_edl_switch`
- **THEN** the service requires an online ADB serial and uses the existing ADB reboot path

#### Scenario: Direct entry fails
- **WHEN** the direct DIAG port returns a protocol, timeout, or identity error
- **THEN** the operation fails with a structured retryable error for the `enter_edl` phase and does not attempt ADB fallback

### Requirement: EDL switching SHALL correlate one physical device

The EDL coordinator SHALL record the original stable identity and physical USB location before entry. It SHALL accept an EDL device only when the VID/PID and physical location match the recorded target, and SHALL reject an absent, changed-location, or ambiguous match within a bounded deadline.

#### Scenario: The same module re-enumerates as EDL
- **WHEN** the normal device disappears and one `05c6:9008` device appears at the recorded physical location
- **THEN** the operation associates the EDL device with the original module and continues

#### Scenario: A different EDL device is present
- **WHEN** an EDL device appears at another physical location or multiple candidates match
- **THEN** the operation fails with a retryable identity-mismatch error and performs no Firehose read

### Requirement: NAND backup SHALL reset and reconnect the module

The read-only NAND backup operation SHALL validate the EDL client, loader, output target, and NAND geometry, run the Firehose `rf` command, send Firehose `reset --resetmode=reset` after the read, and wait for the original USB identity to return at the same physical location before reporting success.

#### Scenario: Read, reset, and reconnect succeed
- **WHEN** a matching EDL device is available, the read produces a geometry-valid image, Firehose reset succeeds, and the original identity returns
- **THEN** the operation atomically publishes the backup file and reports `succeeded`

#### Scenario: Read succeeds but reset fails
- **WHEN** the image passes validation but Firehose reset cannot be acknowledged
- **THEN** the operation reports a failed `reset` phase with `backup_valid=true` and `reconnect_required=true`, and it does not report a complete backup

#### Scenario: Operation is cancelled during EDL
- **WHEN** the user cancels or the application shuts down after EDL entry
- **THEN** the read process is stopped, one bounded cleanup reset is attempted, and the operation reaches the cancelled terminal state without an unbounded worker

### Requirement: Firmware operations SHALL publish bounded recovery details

Device-control EDL and backup operations SHALL retain the asynchronous operation envelope while using the `device_control.*` operation types. Progress messages SHALL use the defined phase labels, and terminal structured errors SHALL identify the phase, retryability, and whether a valid backup file exists without exposing NAND content or sensitive device identifiers.

#### Scenario: Client observes backup progress
- **WHEN** the backup operation advances through entry wait, NAND read, reset, and reconnect
- **THEN** the client receives ordered progress events with the same operation ID and a phase-specific message

#### Scenario: Tool output contains control sequences
- **WHEN** the EDL client writes ANSI progress output or oversized stderr
- **THEN** the operation terminal receives the complete stdout/stderr stream with its ANSI sequences and carriage returns unchanged, while the public error response contains no stdout or stderr

