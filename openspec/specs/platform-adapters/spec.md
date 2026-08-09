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
