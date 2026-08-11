## Purpose

Define the Vue management interface for one device, capability-driven actions, and asynchronous operation state.
## Requirements
### Requirement: The management UI SHALL be a DJOneHub Vue application

The frontend SHALL use Vue 3, TypeScript, Vite, and a state store/service organization for device status, SMS, eSIM, network, raw AT, and VoWiFi workflows without importing the complete VoHive management surface.

#### Scenario: Application starts without hardware
- **WHEN** the web application loads while no device is connected
- **THEN** it renders a single-device offline state and keeps the supported navigation and API connection usable

### Requirement: UI actions SHALL be driven by server capabilities

The frontend SHALL show, disable, or explain actions according to the capability snapshot and SHALL NOT select business behavior from the browser or server operating-system name. The frontend SHALL keep all supported navigation entries visible regardless of the capability snapshot. Feature views SHALL gate executable controls and explain unavailable capabilities inside the view.

#### Scenario: Raw AT is unavailable
- **WHEN** the ready capability snapshot does not include `raw_at`
- **THEN** the UI keeps the Raw AT and firmware navigation entries visible, while their executable controls are disabled or explained in the views

#### Scenario: Device is not connected
- **WHEN** the device is absent, connecting, initializing, degraded, or disconnected
- **THEN** the UI renders all supported navigation entries, while unavailable actions remain disabled or explained inside their views

#### Scenario: A feature capability becomes available
- **WHEN** a ready capability snapshot adds a capability required by a feature
- **THEN** the matching controls become available in the feature view

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

#### Scenario: Feature gating regresses
- **WHEN** a change makes a capability-gated control executable without its capability
- **THEN** an automated feature test fails

#### Scenario: Appearance resolution regresses
- **WHEN** a change resolves a stored or system appearance mode incorrectly
- **THEN** an automated appearance test fails

### Requirement: Views SHALL expose the clear-module-SMS action

The SMS view SHALL provide a control that invokes the already-wired clear-module-SMS capability, gated by the SMS read capability. Clearing SHALL purge the module ME storage while preserving the locally cached inbox display, matching the existing `sms.cleared` semantics.

#### Scenario: Clearing module storage
- **WHEN** the user triggers clear-module-SMS and the request succeeds
- **THEN** the module ME storage is cleared, the cached inbox is preserved, and the result is reported with the clear confirmation message

### Requirement: UI SHALL render asynchronous operations and events

The frontend SHALL associate `operation_id` values with progress and terminal WebSocket events and SHALL resynchronize from a snapshot after a disconnected or out-of-order event stream. The Runtime view SHALL render a dedicated event-source list from runtime diagnostics. The list SHALL include source state, polling interval, and emitted event types alongside the existing topology and message traces. The view SHALL distinguish these sources from transport and cleanup mechanisms.

#### Scenario: Event-source list is shown
- **WHEN** runtime diagnostics are available
- **THEN** the page lists each event-producing worker and its interval and event families

#### Scenario: Event-source state changes
- **WHEN** a source becomes stopped or degraded
- **THEN** its displayed state updates on the next diagnostics refresh without changing the topology contract

#### Scenario: No event sources are available
- **WHEN** diagnostics are unavailable or contain no event sources
- **THEN** the page shows the existing unavailable/empty state without rendering mechanism timers as sources

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

### Requirement: SIM Profile Management SHALL own all local metadata editing

The management UI SHALL list physical SIMs and installed eSIM Profiles in SIM Profile Management and SHALL create or edit local name, phone, notes, and tags only in that view. The view SHALL distinguish device-observed MSISDN from the user-maintained local phone.

#### Scenario: User edits an eSIM Profile's local metadata
- **WHEN** the user changes local name, phone, notes, or tags for an installed eSIM Profile
- **THEN** SIM Profile Management saves the unified SIM Profile resource and both management and eSIM displays converge on that value

#### Scenario: Device reports a different MSISDN
- **WHEN** a later observation updates the modem-reported MSISDN
- **THEN** the UI retains the user's local phone and presents the observed value as separate device data

### Requirement: The eSIM workbench SHALL edit only the eUICC nickname

The eSIM workbench SHALL display local metadata obtained from the SIM Profile registry but SHALL NOT create or edit it. Its Profile editor SHALL retain only the nickname field that invokes the existing eUICC rename action.

#### Scenario: User opens the eSIM Profile editor
- **WHEN** the user edits an installed eSIM Profile from the eSIM workbench
- **THEN** the editor offers the eUICC nickname and no local name, phone, notes, or tags fields

#### Scenario: Local metadata is unavailable
- **WHEN** the SIM Profile registry request fails after a valid eSIM overview loads
- **THEN** the eSIM workbench keeps Profile operations usable and marks only local metadata as unavailable

### Requirement: eSIM Profile loading SHALL not wait for auxiliary reads

The eSIM route SHALL render a valid Profile overview as soon as the overview request completes. SIM Profile metadata, health, pending notifications, and notification history SHALL be isolated auxiliary states and SHALL NOT delay the route's loaded state or erase a valid Profile snapshot when an auxiliary request fails.

#### Scenario: Overview succeeds while health is slow
- **WHEN** the Profile overview returns before the eSIM health request
- **THEN** the route displays Profiles and leaves health in its independent loading state

#### Scenario: Notification listing is slow
- **WHEN** pending notifications are still loading after Profiles are available
- **THEN** the Profile workspace remains usable and the route is not held in its initial loading state

#### Scenario: Auxiliary request fails
- **WHEN** SIM Profile metadata, health, or notification loading fails after a valid overview is returned
- **THEN** the valid Profile snapshot remains visible and only the affected auxiliary state reports failure or unavailability

### Requirement: The UI SHALL present one Device Control view

The management UI SHALL replace separate ADB and EDL configuration/control panels with one Device Control view. The view SHALL load and save one settings document and SHALL present method selection, current mode, tool availability, USB composition controls, NAND backup, and firmware revision together.

#### Scenario: Device Control view loads
- **WHEN** the user opens the device-control route
- **THEN** the UI displays one combined status and settings surface without separate ADB or EDL navigation entries

#### Scenario: Device Control settings are edited
- **WHEN** the user changes an ADB or EDL setting and submits the form
- **THEN** the UI sends the complete settings document and reflects the server's effective values and reasons

### Requirement: Firmware version display SHALL show source and freshness

The Device Control view SHALL display the normalized firmware revision, probe source, and whether the value is live or cached. It SHALL render the server reason when no revision is available.

#### Scenario: QGMR version is displayed
- **WHEN** status reports a revision from `AT+QGMR`
- **THEN** the UI displays the revision and identifies the QGMR source

#### Scenario: Version is cached in EDL
- **WHEN** status reports a cached revision while the device is in EDL
- **THEN** the UI labels it as cached and does not present it as a current live probe

### Requirement: Device-control actions SHALL use the server device-control capabilities

The Device Control view SHALL gate direct EDL, ADB fallback, and complete NAND backup controls using the capability data returned by the server. It SHALL display the server-provided reason for an unavailable method and SHALL not infer support from the browser operating system or from an EDL tool path alone.

The EDL panel SHALL provide a separately confirmed restore-normal-mode action. The NAND panel SHALL provide an optional loader picker and SHALL use a default filename that includes the sanitized firmware revision when one is available.

The ADB panel SHALL provide one command selector for normal reboot and reboot-to-EDL. It SHALL apply the selected command only to the selected online ADB device and SHALL request confirmation before either reboot. The ADB mode control SHALL show only the action that changes the current known state. Its label SHALL state that the mode change restarts the device.

#### Scenario: Direct EDL control is available
- **WHEN** firmware status includes `direct` as an available EDL entry method
- **THEN** the view offers direct EDL entry and submits the method explicitly

#### Scenario: Only ADB fallback is available
- **WHEN** firmware status omits direct EDL but reports an online ADB device
- **THEN** the view requires the selected ADB serial and labels the action as the fallback method

#### Scenario: ADB is enabled
- **WHEN** device-control status reports a known enabled ADB composition
- **THEN** the view shows one full-width `Disable ADB and reboot` action and does not show the enable action

#### Scenario: Complete backup is unavailable
- **WHEN** firmware status reports that read/reset capability is unavailable
- **THEN** the view disables the backup action and renders the supplied reason

### Requirement: Device-control operation UI SHALL show recovery phases

The Device Control operation surface SHALL preserve the last operation snapshot while visible and SHALL render phase-specific progress for EDL entry, NAND read, reset, and reconnect. A valid backup with a failed reset SHALL be shown as an incomplete recovery result, not as a successful finished backup.

#### Scenario: Reset phase fails
- **WHEN** an operation completes with `phase=reset`, `backup_valid=true`, and `reconnect_required=true`
- **THEN** the view shows that the image is valid but the device still needs recovery and keeps the terminal error visible

#### Scenario: Device re-enumerates during entry
- **WHEN** the device status changes to offline while an EDL operation is in progress
- **THEN** the view keeps the operation phase visible and does not offer a second entry action until the operation reaches a terminal state

#### Scenario: Operation has no log output
- **WHEN** an ADB configuration or reboot operation reports progress without operation logs
- **THEN** the view shows progress without rendering an empty terminal

#### Scenario: NAND read streams terminal output
- **WHEN** the EDL client emits stdout or stderr during a NAND read
- **THEN** the view renders the complete stream in xterm and uses the EDL-reported percentage as operation progress

### Requirement: Device Control SHALL render live EDL state and session ownership

The UI SHALL render the server-provided Sahara state, EDL facts, freshness, recovery reason, and lease ownership. It SHALL disable mutating controls when another session owns the device and SHALL not label cached normal-mode data as current EDL firmware.

The UI SHALL store the opaque lease token in `sessionStorage`. It SHALL renew the token before a mutation. It SHALL display masked serial, HWID, PK hash, SBL version, protocol, source, observation time, recovery reason, and active operation when the server provides them.

The UI SHALL display Sahara serial, HWID, PK hash, and SBL fields only when the state is `sahara_identified`. For `detected` or `recovery_required`, it SHALL show the state and a pending or failure reason instead of presenting empty values as facts.

#### Scenario: Another browser controls EDL
- **WHEN** status reports that another client owns the lease
- **THEN** the UI shows the live state and disables NAND backup, reset, and mode mutations

### Requirement: NAND backup SHALL not imply reset

After backup success, the UI SHALL show that the device remains in EDL and SHALL offer reset as a separate confirmed action.

#### Scenario: Backup completes
- **WHEN** the NAND backup operation succeeds
- **THEN** the UI reports a valid backup, keeps EDL mode visible, and does not show reconnect as part of backup

