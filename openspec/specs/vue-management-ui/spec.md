## Purpose

Define the Vue management interface for one device, capability-driven actions, and asynchronous operation state.

## Requirements

### Requirement: The management UI SHALL be a DJOneHub Vue application

The frontend SHALL use Vue 3, TypeScript, Vite, and a state store/service organization for device status, SMS, eSIM, network, raw AT, and VoWiFi workflows without importing the complete VoHive management surface.

#### Scenario: Application starts without hardware
- **WHEN** the web application loads while no device is connected
- **THEN** it renders a single-device offline state and keeps the supported navigation and API connection usable

### Requirement: UI actions SHALL be driven by server capabilities

The frontend SHALL show, disable, or explain actions according to the capability snapshot and SHALL NOT select business behavior from the browser or server operating-system name. This gating SHALL apply to navigation as well as in-view controls: a navigation entry that declares a `capability` the snapshot does not include SHALL be hidden, so users cannot open a view they are not permitted to use and then receive a transport error.

#### Scenario: Raw AT is unavailable
- **WHEN** the capability snapshot does not include `raw_at`
- **THEN** the UI does not present an executable raw AT action and displays the server-provided reason when the feature is inspected

#### Scenario: Navigation entry requires a missing capability
- **WHEN** a navigation entry declares a `capability` that the snapshot does not include
- **THEN** the entry is not rendered in the navigation, instead of being clickable and producing a permission error

### Requirement: Views SHALL expose the clear-module-SMS action

The SMS view SHALL provide a control that invokes the already-wired clear-module-SMS capability, gated by the SMS read capability. Clearing SHALL purge the module ME storage while preserving the locally cached inbox display, matching the existing `sms.cleared` semantics.

#### Scenario: Clearing module storage
- **WHEN** the user triggers clear-module-SMS and the request succeeds
- **THEN** the module ME storage is cleared, the cached inbox is preserved, and the result is reported with the clear confirmation message

### Requirement: UI SHALL render asynchronous operations and events

The frontend SHALL associate `operation_id` values with progress and terminal WebSocket events and SHALL resynchronize from a snapshot after a disconnected or out-of-order event stream.

#### Scenario: eSIM progress arrives
- **WHEN** an eSIM operation is accepted by REST and progress events arrive over WebSocket
- **THEN** the UI shows the operation state and final result using the same operation identity

### Requirement: Management UI state SHALL be domain-owned and typed

The management UI SHALL organize view state in domain-specific stores instead of one root component, and SHALL expose view data through a typed `ViewContext` contract rather than an untyped record. The shell SHALL load the data for the active view on startup and on every navigation, not only when a domain event happens to arrive.

#### Scenario: A view consumes its domain state
- **WHEN** a routed view needs device, SMS, eSIM, network, or VoWiFi state
- **THEN** it reads it from the corresponding domain store through a typed view context instead of the root component's untyped record

#### Scenario: The application opens directly on a non-overview view
- **WHEN** the web application loads with the route pointing at SMS, eSIM, network, calls, VoWiFi, settings, or notifications
- **THEN** the active view's loader runs on startup so its content populates without depending on an unrelated domain event

#### Scenario: View context is type-checked
- **WHEN** a view accesses the view context at compile time
- **THEN** the members are type-checked by TypeScript instead of being accessed through a `Record<string, any>` escape hatch

### Requirement: The active view SHALL load on startup and on status changes

The shell SHALL trigger the active view's loader during `onMounted` (or an equivalent startup hook) and SHALL schedule a refresh of the active view when a `device.status.changed` event arrives, regardless of which view is active. The UI SHALL NOT leave a view in a permanent loading state when the device is connected and ready.

#### Scenario: Direct load of the SMS view
- **WHEN** the application loads with the route set to the SMS view and the device is ready
- **THEN** the SMS loader runs on startup and the conversation list renders instead of spinning indefinitely

#### Scenario: Device becomes ready while a non-overview view is active
- **WHEN** a `device.status.changed` event arrives while the network view is active
- **THEN** the network view refreshes so its status reflects the now-ready device

### Requirement: Traffic range controls SHALL show a loading state

The overview traffic panel SHALL indicate progress while fetching a week or month range, so the history table is not shown empty and unresponsive during the request.

#### Scenario: Switching to the week range
- **WHEN** the user switches the traffic range from day to week
- **THEN** the panel shows a loading indicator until the range data returns, then renders the history

### Requirement: Management UI SHALL keep client state bounded

The frontend SHALL remove operation entries from its client-side operations map after they reach a terminal state, and SHALL back off reconnection attempts exponentially with jitter and a bounded maximum delay.

#### Scenario: Operations accumulate in a long session
- **WHEN** a session performs many operations over a long period
- **THEN** terminal-state operations are removed from the map so client memory stays bounded

#### Scenario: Connection flaps repeatedly
- **WHEN** the WebSocket connection fails repeatedly
- **THEN** reconnect delays grow exponentially with jitter and never exceed the configured maximum

### Requirement: Operation status surfaces SHALL stay stable while visible

The firmware operation modal and the SMS send-status indicator SHALL retain a snapshot of the operation while they are open or in progress, so they do not blank out when the client-side operation entry is pruned after its terminal-state TTL. This SHALL NOT change the bounded-state cleanup policy for the operations map.

#### Scenario: Firmware modal left open past the cleanup TTL
- **WHEN** the firmware operation modal is open and the underlying operation entry is removed by the client-side TTL cleanup
- **THEN** the modal still shows the last-known operation state and result

#### Scenario: SMS status indicator after send
- **WHEN** an SMS send operation completes and the user remains on the conversation
- **THEN** the send-status indicator continues to reflect the completed state for a reasonable time instead of disappearing

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

### Requirement: The eSIM view SHALL support profile disable and modern download input

The eSIM view SHALL present a disable action for profiles in the enabled state, symmetric with the existing enable action, and SHALL run it through the same operation/progress channel. The download dialog SHALL accept activation codes from QR scan or image paste in addition to manual text entry, and SHALL present the interactive confirmation-code prompt: when a download operation publishes a confirmation-code request, the view SHALL show an input dialog and send the reply, or offer cancellation.

#### Scenario: Enabled profile can be disabled
- **WHEN** a profile card shows the enabled state
- **THEN** the view offers a disable action that submits the disable operation and renders its progress

#### Scenario: Disabled profile does not offer disable
- **WHEN** a profile card shows a non-enabled state
- **THEN** the view does not offer the disable action, matching the existing enable-action symmetry

#### Scenario: Activation code is scanned
- **WHEN** the user scans a QR code or pastes an image containing an `LPA:1$...` activation code into the download dialog
- **THEN** the dialog fills the activation code field and the user can review and submit it

#### Scenario: Download requests a confirmation code
- **WHEN** the download operation publishes a confirmation-code request event
- **THEN** the view shows an input dialog bound to that operation; submitting sends the code and cancelling aborts the download

### Requirement: The eSIM view SHALL manage pending notifications

The eSIM view SHALL provide a notifications panel that lists pending eUICC notifications with their event type and profile identifier, and SHALL offer retry and remove actions per notification. The panel SHALL refresh when the eSIM data is refreshed and SHALL show per-action results or structured errors inline.

#### Scenario: Pending notifications are shown
- **WHEN** the user opens the notifications panel and the API returns pending notifications
- **THEN** each notification is listed with its event type and profile identifier

#### Scenario: Notification is retried
- **WHEN** the user retries a pending notification and the API reports success
- **THEN** the panel removes the notification from the list and shows the success result

#### Scenario: Notification retry fails
- **WHEN** the user retries a pending notification and the API reports a structured error
- **THEN** the panel keeps the notification and shows the error inline

#### Scenario: Notification is removed
- **WHEN** the user removes a pending notification and the API confirms it
- **THEN** the panel drops the notification from the list
