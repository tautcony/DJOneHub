## ADDED Requirements

### Requirement: The management UI SHALL be a DJOneHub Vue application

The frontend SHALL use Vue 3, TypeScript, Vite, and a state store/service organization for device status, SMS, eSIM, network, raw AT, and VoWiFi workflows without importing the complete VoHive management surface.

#### Scenario: Application starts without hardware
- **WHEN** the web application loads while no device is connected
- **THEN** it renders a single-device offline state and keeps the supported navigation and API connection usable

### Requirement: UI actions SHALL be driven by server capabilities

The frontend SHALL show, disable, or explain actions according to the capability snapshot and SHALL NOT select business behavior from the browser or server operating-system name.

#### Scenario: Raw AT is unavailable
- **WHEN** the capability snapshot does not include `raw_at`
- **THEN** the UI does not present an executable raw AT action and displays the server-provided reason when the feature is inspected

### Requirement: UI SHALL render asynchronous operations and events

The frontend SHALL associate `operation_id` values with progress and terminal WebSocket events and SHALL resynchronize from a snapshot after a disconnected or out-of-order event stream.

#### Scenario: eSIM progress arrives
- **WHEN** an eSIM operation is accepted by REST and progress events arrive over WebSocket
- **THEN** the UI shows the operation state and final result using the same operation identity
