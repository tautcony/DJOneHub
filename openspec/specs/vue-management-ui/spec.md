## Purpose

Define the Vue management interface for one device, capability-driven actions, and asynchronous operation state.

## Requirements

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

### Requirement: Management UI state SHALL be domain-owned and typed

The management UI SHALL organize view state in domain-specific stores instead of one root component, and SHALL expose view data through a typed `ViewContext` contract rather than an untyped record.

#### Scenario: A view consumes its domain state
- **WHEN** a routed view needs device, SMS, eSIM, network, or VoWiFi state
- **THEN** it reads it from the corresponding domain store through a typed view context instead of the root component's untyped record

#### Scenario: View context is type-checked
- **WHEN** a view accesses the view context at compile time
- **THEN** the members are type-checked by TypeScript instead of being accessed through a `Record<string, any>` escape hatch

### Requirement: Management UI SHALL keep client state bounded

The frontend SHALL remove operation entries from its client-side operations map after they reach a terminal state, and SHALL back off reconnection attempts exponentially with jitter and a bounded maximum delay.

#### Scenario: Operations accumulate in a long session
- **WHEN** a session performs many operations over a long period
- **THEN** terminal-state operations are removed from the map so client memory stays bounded

#### Scenario: Connection flaps repeatedly
- **WHEN** the WebSocket connection fails repeatedly
- **THEN** reconnect delays grow exponentially with jitter and never exceed the configured maximum

### Requirement: Management UI SHALL localize consistently

The UI SHALL keep the document language attribute in sync with the active locale, SHALL resolve operation and status keys through the i18n catalog with a fallback instead of rendering raw keys, and SHALL use a dedicated i18n namespace for VoWiFi operations instead of reusing the eSIM namespace.

#### Scenario: Locale changes to Chinese
- **WHEN** the user switches the locale to Chinese
- **THEN** the document language attribute updates to the matching language tag and UI text is rendered in Chinese

#### Scenario: Unknown status key is rendered
- **WHEN** the UI receives an operation or call status key that is missing from the current locale
- **THEN** it renders the i18n fallback text instead of the raw key

#### Scenario: VoWiFi operation text is looked up
- **WHEN** the UI displays a VoWiFi operation state
- **THEN** the text is resolved from the VoWiFi operation namespace rather than reusing eSIM operation keys
