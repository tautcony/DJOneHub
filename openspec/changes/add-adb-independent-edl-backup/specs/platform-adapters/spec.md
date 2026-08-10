## ADDED Requirements

### Requirement: Platform adapters SHALL expose verified EDL ports

An adapter that supports direct EDL entry SHALL expose a platform port that enumerates the active USB configuration, claims only the verified DIAG interface, sends `4B 65 01 00 54 0F 7E`, decodes the returned HDLC frame, verifies its CRC, and requires its payload to equal the exact seven-byte request. AT discovery SHALL not send probe commands to the DIAG interface. The adapter SHALL expose no direct EDL capability until interface and endpoint selection has host-safe fixture coverage.

#### Scenario: macOS DIAG switch is acknowledged
- **WHEN** a supported DJI or Quectel USB device is found at the requested physical location and the DIAG interface echoes the frame
- **THEN** the adapter validates the HDLC-wrapped echo, returns success, and releases the USB interface after the device begins re-enumeration

#### Scenario: DIAG echo is incomplete
- **WHEN** the USB read times out or returns bytes other than the exact seven-byte echo
- **THEN** the adapter returns a transport/protocol error and does not claim that EDL entry succeeded

#### Scenario: Unsupported platform is built
- **WHEN** a platform has no verified DIAG implementation
- **THEN** its capability snapshot omits `firmware_edl_switch` and the port returns `capability_not_supported`

### Requirement: Platform discovery SHALL preserve physical location across USB modes

EDL and original-mode discovery SHALL expose a stable physical-location key suitable for one-device correlation. Discovery SHALL distinguish identical VID/PID devices at different locations and SHALL return an ambiguity error when the target cannot be selected uniquely.

#### Scenario: Two identical modules are connected
- **WHEN** two supported devices have the same VID/PID but different physical locations
- **THEN** a firmware operation selects only the requested location and never sends a frame to the other device

#### Scenario: Physical location is not available
- **WHEN** a platform cannot provide a stable location for an EDL candidate
- **THEN** the platform omits the direct EDL capability for that build or returns a structured identity error
