## Purpose

Define the operating-system abstraction boundaries and verified adapter behavior for supported desktop platforms.
## Requirements
### Requirement: Shared code SHALL access operating-system functions through ports

Shared domain, application, runtime, API, and backend code SHALL NOT directly execute `ifconfig`, `networksetup`, `ip`, PowerShell, platform service commands, or hardcoded device paths; it SHALL use discovery, serial, network, tunnel, and service ports.

#### Scenario: Network mode changes
- **WHEN** an application service changes USB network mode
- **THEN** it invokes `NetworkController` and does not construct or execute an operating-system command itself

### Requirement: Each target platform SHALL provide explicit adapters

Linux, macOS, and Windows adapters SHALL implement the applicable device discovery, serial/USB transport, network, packet-tunnel, and service interfaces and SHALL register only verified capabilities.

#### Scenario: Platform lacks a feature
- **WHEN** a platform adapter cannot implement a requested capability
- **THEN** it returns a structured unsupported result and the runtime excludes that capability from the snapshot

### Requirement: Platform builds SHALL report truthful capability coverage

Each supported platform build MUST report which adapter capabilities are available, degraded, or unavailable. The repository test suite MUST use host-valid fixtures so supported host builds can verify that reporting without failing on foreign filesystem or environment semantics.

#### Scenario: Linux build reports available capabilities
- **WHEN** the application starts on Linux with native network and power adapters compiled in
- **THEN** runtime diagnostics report those capabilities as available and do not expose Windows-only placeholder adapters

#### Scenario: Windows build reports degraded or unavailable capabilities explicitly
- **WHEN** the application starts on Windows and a native capability is not implemented
- **THEN** runtime diagnostics identify that capability as degraded or unavailable instead of reporting a silent success

#### Scenario: Windows-hosted tests use representable fixtures
- **WHEN** the repository Go test suite runs on Windows
- **THEN** tests use Windows-resolvable executables and environment seams, and Linux-only sysfs path-shape tests skip without suppressing platform-independent adapter coverage

#### Scenario: Windows has basic MBIM only
- **WHEN** the Windows adapter verifies device discovery and MBIM but not eSIM or VoWiFi data plane
- **THEN** the capability snapshot reports only the verified basic features and the UI/API reject unverified operations consistently

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

