## ADDED Requirements

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

### Requirement: Platform builds SHALL report verified capability coverage

The application SHALL start on Linux, macOS, and Windows build targets that are supported by the repository and SHALL expose platform, adapter, and hardware capability information without claiming unverified features.

#### Scenario: Windows has basic MBIM only
- **WHEN** the Windows adapter verifies device discovery and MBIM but not eSIM or VoWiFi data plane
- **THEN** the capability snapshot reports only the verified basic features and the UI/API reject unverified operations consistently
