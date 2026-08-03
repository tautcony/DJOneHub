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
