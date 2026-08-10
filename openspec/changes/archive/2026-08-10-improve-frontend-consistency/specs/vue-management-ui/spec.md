## ADDED Requirements

## MODIFIED Requirements

### Requirement: UI actions SHALL be driven by server capabilities

The frontend SHALL show, disable, or explain actions according to the capability snapshot and SHALL NOT select business behavior from the browser or server operating-system name. When the device is ready, a navigation entry that declares a capability absent from the snapshot SHALL be hidden. When the device is absent, connecting, initializing, degraded, or disconnected, the frontend SHALL keep all supported navigation entries visible and SHALL gate executable controls inside their views.

#### Scenario: Raw AT is unavailable on a ready device
- **WHEN** the ready capability snapshot does not include `raw_at`
- **THEN** the UI does not render the Raw AT or firmware navigation entries

#### Scenario: Device is not connected
- **WHEN** the device is absent, connecting, initializing, degraded, or disconnected
- **THEN** the UI renders all supported navigation entries, while unavailable actions remain disabled or explained inside their views

#### Scenario: Navigation capability becomes available
- **WHEN** a ready capability snapshot adds a capability required by a navigation entry
- **THEN** the matching navigation entry becomes visible

### Requirement: Management UI SHALL apply the selected appearance mode

The management UI SHALL provide light, dark, and system appearance modes. The UI SHALL apply the resolved mode to Ant Design Vue and project CSS tokens.

#### Scenario: User selects dark mode
- **WHEN** the user selects dark appearance mode
- **THEN** the application persists the preference and renders all current views with the dark theme

#### Scenario: User selects system mode
- **WHEN** the user selects system appearance mode and the operating-system preference changes
- **THEN** the application updates the resolved theme without changing the stored system preference

#### Scenario: Application reloads
- **WHEN** the application reloads after the user selected an appearance mode
- **THEN** the application restores the stored preference before it renders the management shell

### Requirement: Management UI SHALL use shared interaction semantics

The management UI SHALL use shared semantic tones for device status. Destructive workflow confirmations SHALL use an application-styled confirmation surface.

#### Scenario: Firmware status is shown
- **WHEN** the firmware view reports an enabled or connected status
- **THEN** the view uses the shared success tone instead of a private status color

#### Scenario: Destructive action needs confirmation
- **WHEN** a firmware or network action can interrupt device operation
- **THEN** the UI shows a localized application confirmation before it sends the request

#### Scenario: Page header has an eyebrow
- **WHEN** the application supplies an eyebrow to the shared page header
- **THEN** the page header renders the eyebrow visibly

### Requirement: Frontend changes SHALL pass the project quality checks

The frontend SHALL provide automated type, lint, format, test, and production-build checks. Continuous integration SHALL run these checks.

#### Scenario: Frontend source is checked
- **WHEN** continuous integration checks the frontend source
- **THEN** type, lint, format, test, and build commands complete without errors or warnings

#### Scenario: Capability filtering regresses
- **WHEN** a change makes a capability-gated navigation item visible without its capability
- **THEN** an automated navigation test fails

#### Scenario: Appearance resolution regresses
- **WHEN** a change resolves a stored or system appearance mode incorrectly
- **THEN** an automated appearance test fails
