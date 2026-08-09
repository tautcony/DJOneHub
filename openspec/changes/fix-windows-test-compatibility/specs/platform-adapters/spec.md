## MODIFIED Requirements

### Requirement: Platform builds SHALL report truthful capability coverage
Each supported platform build MUST report which adapter capabilities are available, degraded, or unavailable, and the repository test suite MUST use host-valid fixtures so supported host builds can verify that reporting without failing on foreign filesystem or environment semantics.

#### Scenario: Linux build reports available capabilities
- **WHEN** the application starts on Linux with native network and power adapters compiled in
- **THEN** runtime diagnostics report those capabilities as available and do not expose Windows-only placeholder adapters

#### Scenario: Windows build reports degraded or unavailable capabilities explicitly
- **WHEN** the application starts on Windows and a native capability is not implemented
- **THEN** runtime diagnostics identify that capability as degraded or unavailable instead of reporting a silent success

#### Scenario: Windows-hosted tests use representable fixtures
- **WHEN** the repository Go test suite runs on Windows
- **THEN** tests use Windows-resolvable executables and environment seams, and Linux-only sysfs path-shape tests skip without suppressing platform-independent adapter coverage
