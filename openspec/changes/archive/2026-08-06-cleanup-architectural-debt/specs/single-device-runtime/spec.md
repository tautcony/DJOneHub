## ADDED Requirements

### Requirement: Device discovery SHALL probe only candidates the runtime consumes

The runtime SHALL declare its single-device constraint explicitly, and platform discovery SHALL probe only the candidate the runtime will actually consume, so probing work matches the consumption contract across platforms instead of probing unused candidates.

#### Scenario: Linux discovery finds multiple candidates
- **WHEN** Linux discovery identifies several serial candidates but the runtime consumes only the selected one
- **THEN** only the candidate that will be used is probed, and the remaining candidates are not subjected to probing work

#### Scenario: Platform contract is inspected
- **WHEN** the discovery contract between runtime and platform adapters is reviewed
- **THEN** all platforms probe consistently with the runtime's single-device consumption instead of asymmetric behavior such as one platform probing everything and another stopping at the first responder

### Requirement: Device rescans SHALL be serialized with the runtime lifecycle

HTTP-triggered rescans and the polling-loop scan SHALL be serialized through one scan path so a concurrent scan can never re-install a backend that the lifecycle already closed.

#### Scenario: Rescan races the polling loop
- **WHEN** an HTTP rescan request arrives while the polling loop is scanning or while the lifecycle is closing a backend
- **THEN** the scans run serialized and the closed backend is not re-installed into the runtime by the concurrent scan
