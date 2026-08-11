## Purpose

Define lifecycle ownership, identity correlation, and serialized resource access for the one managed physical device.
## Requirements
### Requirement: The runtime SHALL manage one physical device lifecycle

The system SHALL represent one managed device through `absent`, `discovered`, `connecting`, `initializing`, `ready`, `degraded`, and `disconnected` states, and SHALL publish state changes from one runtime owner.

#### Scenario: Device reaches ready
- **WHEN** a supported device is discovered and its selected backend initializes successfully
- **THEN** the runtime transitions through connection and initialization to `ready` and publishes a capability snapshot

#### Scenario: Device is removed
- **WHEN** the managed device disappears or its transport is irrecoverably closed
- **THEN** the runtime cancels active operations, closes backend resources, enters `disconnected`, and starts eligible rediscovery

### Requirement: The runtime SHALL correlate re-enumerated ports to the same device

The system SHALL use stable identity inputs such as physical location, VID/PID, IMEI, and serial number to correlate USB mode changes and port changes without introducing a multi-device registry.

#### Scenario: USB mode changes ports
- **WHEN** a device temporarily disappears and reappears with a different set of ports
- **THEN** the runtime associates the ports with the same managed device when stable identity inputs match

### Requirement: The runtime SHALL serialize conflicting device operations

The system SHALL coordinate AT, QMI, MBIM, APDU, network, and VoWiFi operations with resource locks and cancellation so conflicting operations cannot use the same exclusive resource concurrently.

#### Scenario: APDU conflicts with SIM reset
- **WHEN** an APDU operation holds the SIM resource and a reset requiring that resource is submitted
- **THEN** the second operation waits or fails with a structured conflict result and does not access the resource concurrently

### Requirement: The application SHALL shut down in reverse start order through one path

UI-exit and signal-driven shutdown SHALL converge on a single shutdown path. The shutdown path SHALL close application and operation admission before draining HTTP handlers, cancel and join in-flight operations, then stop each background worker in reverse of the actual start order (Notification, Extras, Network, SMS, Runtime) and wait for each to join before closing storage. HTTP draining and worker stopping SHALL use separate bounded contexts. If a worker does not stop by its deadline, shutdown SHALL return an error and SHALL NOT close storage while that worker can still write. It SHALL keep the native UI alive until notification sink calls have returned, SHALL NOT deliver events to a native UI that has already stopped, and SHALL NOT start the native UI when the HTTP server failed to start.

#### Scenario: Signal arrives while the UI is running
- **WHEN** a termination signal arrives while the native UI is running
- **THEN** exactly one shutdown sequence runs: workers stop and join in reverse start order, then storage closes, and no event is delivered to the stopped UI afterwards

#### Scenario: Poller writes during shutdown
- **WHEN** a polling service is mid-refresh while the application shuts down
- **THEN** shutdown waits for the poller to stop before closing the store, so no in-flight write races with the closed database

#### Scenario: Operation is running during shutdown
- **WHEN** shutdown begins while an asynchronous operation is running
- **THEN** new operations are rejected, the running operation is cancelled and joined before storage closes, and its terminal state is `cancelled`

#### Scenario: HTTP server fails to start
- **WHEN** the HTTP server fails to listen
- **THEN** the application exits through the normal shutdown path and the native UI is not started against an unreachable URL

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

### Requirement: Runtime capability snapshots SHALL include verified platform firmware capabilities

The runtime SHALL compose the backend capability set with the active platform adapter's verified firmware capabilities. It SHALL retain the reason string for every capability and SHALL omit direct EDL or complete-backup capabilities when the platform, tool, loader, or reset path is unverified.

#### Scenario: macOS supports direct EDL and reset
- **WHEN** the active platform adapter verifies the DIAG switch and the configured Firehose client verifies read and reset
- **THEN** the ready-device snapshot includes `firmware_edl_switch` and `firmware_nand_backup` with truthful reasons

#### Scenario: Platform capability is unavailable
- **WHEN** the active adapter has no verified direct DIAG implementation
- **THEN** the snapshot omits `firmware_edl_switch` and exposes the adapter reason through firmware status without a fabricated success capability

### Requirement: Runtime identity SHALL survive EDL and boot re-enumeration

The runtime SHALL keep the original stable identity and physical location as the correlation anchor while the device temporarily disappears for EDL entry or Firehose reset. It SHALL not install a backend for a different location or ambiguous candidate.

#### Scenario: Original device returns after reset
- **WHEN** the original USB identity returns at the recorded physical location after Firehose reset
- **THEN** the runtime reconnects the same managed device and publishes the normal ready snapshot

#### Scenario: A different device returns first
- **WHEN** a candidate with a different physical location or stable identity appears during the reconnect deadline
- **THEN** the runtime rejects it for the current operation and keeps the managed device unavailable until the original target is found or the deadline expires

### Requirement: Runtime SHALL preserve EDL session identity

The runtime SHALL correlate normal and EDL candidates by stable identity and physical location. It SHALL reject ambiguous or changed-location matches and SHALL enter recovery-required state after bounded observation or reconnect failure.

For an EDL-only cold start, the runtime SHALL establish a session only after the platform adapter finds one unique EDL candidate. After explicit reset, it SHALL match a supported normal USB identity at the same physical location.

#### Scenario: EDL candidate moves location
- **WHEN** an EDL candidate appears at a different physical location
- **THEN** the runtime rejects it and marks the managed session as recovery-required

