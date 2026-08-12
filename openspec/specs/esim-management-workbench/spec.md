## Purpose

Define the verified interaction, state, safety, and accessibility requirements for the eSIM management workbench.
## Requirements
### Requirement: Every workbench action SHALL be backed by a verified existing capability
Before replacing the existing eSIM view, the implementation SHALL verify the current service and API contracts for Profile overview, download, enable, disable, rename, delete, local notes, pending notifications, notification process/removal, notification history, operation progress, and dynamic confirmation-code replies. The redesigned UI SHALL NOT expose an executable command that lacks a working current contract, and a failed verification SHALL be fixed or the corresponding command SHALL remain unavailable with an explanation.

#### Scenario: Existing action passes contract verification
- **WHEN** an existing eSIM action returns the documented result and its state or operation events converge in a focused test
- **THEN** the redesigned workbench may expose that action using the existing contract

#### Scenario: Existing action fails verification
- **WHEN** an action exists in code but its service/API test fails or its terminal state cannot be observed
- **THEN** implementation stops treating the action as ready, fixes the blocking defect or disables the entry, and does not simulate success in the UI

#### Scenario: MiniLPA feature has no current DJOneHub contract
- **WHEN** a referenced MiniLPA feature such as default SM-DP+ editing, notification behavior preferences, or notification batching has no verified DJOneHub API
- **THEN** the redesigned page does not add an executable placeholder for that feature

### Requirement: The eSIM route SHALL present one interaction-focused workbench
The management UI SHALL keep eSIM management on the existing route and SHALL organize current capabilities into page-local `Profiles` and `Notifications` workspaces under one shared eSIM summary. Profiles SHALL be the default workspace, Notifications SHALL expose the pending count, and the change SHALL NOT add global navigation entries or alter non-eSIM pages.

#### Scenario: User opens eSIM management
- **WHEN** a user navigates to the existing eSIM route with eSIM capability available
- **THEN** the page shows the shared summary and Profiles workspace without navigating away from the route

#### Scenario: Pending notifications exist
- **WHEN** the current notification snapshot contains pending entries
- **THEN** the Notifications workspace label shows their count and can be opened without a route change

#### Scenario: eSIM capability is unavailable
- **WHEN** the connected backend does not advertise eSIM capability
- **THEN** the route explains that eSIM management is unavailable and exposes no executable card action

### Requirement: The workbench SHALL summarize only currently available eSIM data
The summary SHALL use fields already returned by the verified overview, health, and notification contracts, including masked EID, active Profile or Profile count, current operational health when valid, and pending notification count. It SHALL provide refresh and download commands and SHALL distinguish an unreadable card from a readable eUICC with no Profiles. It SHALL NOT promise chip details that the current API does not return.

#### Scenario: eUICC has an enabled Profile
- **WHEN** overview contains an enabled Profile and the health response is valid
- **THEN** the summary identifies the active Profile and presents the verified operational state

#### Scenario: eUICC has no Profiles
- **WHEN** overview reports a readable eUICC with an empty Profile list
- **THEN** the page shows an empty Profile state with a download action rather than an unavailable-card warning

#### Scenario: Health data is unavailable
- **WHEN** the optional health request fails while Profile overview remains readable
- **THEN** Profile management remains usable and the summary omits or marks health data unavailable without claiming a failure for the whole page

### Requirement: Users SHALL be able to find and operate existing Profiles efficiently
The Profiles workspace SHALL support local search by nickname, service provider, ICCID, and local tags, plus enabled/disabled filtering. Each Profile item SHALL show the currently available nickname, provider, state, masked ICCID, Profile class, and local metadata. It SHALL expose only valid existing actions: Enable for disabled Profiles, Disable for enabled Profiles, Rename/local-note editing, related notifications, and Delete only for non-enabled Profiles.

#### Scenario: User searches Profiles
- **WHEN** the user enters a nickname, provider, ICCID fragment, or local tag
- **THEN** the displayed Profile set is filtered locally without issuing a device request

#### Scenario: Profile is enabled
- **WHEN** a Profile has enabled state
- **THEN** its state is visually and textually clear, Disable is the primary state action, and Delete is unavailable

#### Scenario: Profile is disabled
- **WHEN** a Profile has disabled state
- **THEN** Enable is the primary state action and Delete is available only behind a destructive confirmation

#### Scenario: User opens related notifications
- **WHEN** the user invokes related notifications for a Profile
- **THEN** the workbench switches to Notifications and filters current pending/history entries by that ICCID

### Requirement: Card nickname and local Profile metadata SHALL remain distinct
The Profile editor SHALL distinguish the nickname written to the eUICC from DJOneHub-local label, phone, and tags. It SHALL submit only changed values where the existing contracts permit and SHALL report rename and local-note failures accurately rather than treating the two persistence targets as one opaque save.

#### Scenario: Only local metadata changes
- **WHEN** the user changes local label, phone, or tags without changing the card nickname
- **THEN** the UI saves the local note and does not invoke Profile rename

#### Scenario: Card rename fails
- **WHEN** the eUICC rename request fails
- **THEN** the editor remains open or otherwise preserves input, reports the rename failure, and does not claim the complete edit succeeded

### Requirement: Pending notifications and notification history SHALL be separate views
The Notifications workspace SHALL separate pending eUICC notifications from persisted notification history. Pending entries SHALL support local text, event, and Profile filtering plus the verified individual Process and Remove commands. History SHALL be read-only and support local state, event, Profile, and text filtering. The UI SHALL use human-readable event and state labels instead of raw keys.

#### Scenario: User filters pending notifications
- **WHEN** the user chooses an event or Profile filter
- **THEN** only matching pending entries are displayed and no notification operation is issued

#### Scenario: User processes one pending notification
- **WHEN** the user invokes Process on a retryable pending notification
- **THEN** the existing process endpoint is called once, the busy state is scoped to that entry or command, and pending/history snapshots refresh after completion

#### Scenario: User removes one pending notification
- **WHEN** the user confirms Remove on a pending notification
- **THEN** the existing remove endpoint is called once and the UI explains that removing the eUICC queue entry cannot be undone

#### Scenario: User follows a notification to its Profile
- **WHEN** a notification references an installed Profile and the user invokes View Profile
- **THEN** the workbench switches to Profiles and focuses or filters to that ICCID

### Requirement: Profile download SHALL be one continuous presentation of existing behavior
The download interaction SHALL keep activation-code input, QR image selection, clipboard image paste, drag-and-drop parsing, optional confirmation/matching fields, operation progress, and dynamic confirmation-code requests in one coherent dialog or drawer. The backend SHALL remain authoritative for activation-code validation, and the flow SHALL continue to use the current download and confirmation reply endpoints without changing their protocol.

#### Scenario: User supplies an activation code manually
- **WHEN** the user enters an activation code and submits a valid current request
- **THEN** the existing download endpoint returns an operation ID and the same presentation transitions to operation progress

#### Scenario: User drops or pastes a QR image
- **WHEN** a decodable activation-code image is dropped, selected, or pasted
- **THEN** the existing `jsqr` path populates the input and allows the user to review it before submission

#### Scenario: QR image cannot be decoded
- **WHEN** an image contains no decodable activation code
- **THEN** the flow reports the decoding failure, preserves manual input, and does not start an operation

#### Scenario: Download requests a confirmation code
- **WHEN** the current operation publishes `esim.confirmation_code_request`
- **THEN** the same download presentation accepts or declines the code for that operation and then resumes progress or displays cancellation

### Requirement: Asynchronous eSIM operations SHALL retain visible context until convergence
Enable, Disable, Delete, and Download SHALL remain associated with the operation ID returned by the current API. The workbench SHALL show accepted, running, progress, terminal, and failure states in a stable operation area, disable conflicting actions while appropriate, and refresh affected snapshots after terminal success. Closing a transient dialog SHALL NOT erase an active operation from the page.

#### Scenario: Profile action is accepted
- **WHEN** Enable, Disable, Delete, or Download returns an operation ID
- **THEN** the workbench displays that operation and does not imply the device state changed merely because the request was accepted

#### Scenario: Operation succeeds
- **WHEN** the operation reaches a succeeded terminal state
- **THEN** the workbench refreshes Profile, health, pending notification, and history snapshots that are supported by current contracts

#### Scenario: Operation fails
- **WHEN** an operation reaches a failed or cancelled terminal state
- **THEN** the workbench retains the relevant context, shows the structured failure, and makes a safe retry path available

#### Scenario: Event stream reconnects
- **WHEN** WebSocket events are interrupted while an eSIM operation is active
- **THEN** the UI uses the existing operation status endpoint and refreshed snapshots to converge instead of creating a second operation

### Requirement: Sensitive identifiers and destructive actions SHALL remain protected
EID, ICCID, AID, and related identifiers SHALL follow the existing sensitive-information setting in the summary, Profile, notification, and dialog surfaces. Profile deletion and notification removal SHALL require target-specific confirmation and explain their irreversible effects. State SHALL not be conveyed by color alone.

#### Scenario: Sensitive details are hidden
- **WHEN** the existing sensitive-information setting is off
- **THEN** identifiers remain masked in all redesigned eSIM surfaces and are not leaked through labels, tooltips, URLs, or operation messages

#### Scenario: User attempts to delete an enabled Profile
- **WHEN** a Profile is enabled
- **THEN** no executable Delete command is available and the UI directs the user to disable it first

### Requirement: The workbench SHALL be responsive and keyboard usable
The shared summary, tab controls, filters, Profile items, notification rows, operation area, and dialogs SHALL use stable dimensions and responsive constraints so they do not overlap on supported viewport sizes. Icon-only commands SHALL have accessible names and tooltips, and keyboard focus order SHALL follow the visible workflow.

#### Scenario: Workbench is viewed on a narrow viewport
- **WHEN** the viewport cannot fit the desktop Profile grid or one-line toolbar
- **THEN** content reflows to one column, actions remain reachable, and text does not overlap adjacent controls

#### Scenario: User navigates with a keyboard
- **WHEN** the user tabs through workspaces, filters, Profile actions, notification actions, and the download flow
- **THEN** every control is reachable in visual order and every icon-only action has an accessible name

### Requirement: eSIM overview and health SHALL share snapshots

One public eSIM overview request SHALL use one eSIM snapshot path. eSIM health
SHALL reuse the current device status snapshot and eSIM snapshot and SHALL NOT
force a second complete hardware read.

#### Scenario: eSIM overview is loaded cold

- **WHEN** the business backend supports the rich eSIM snapshot port
- **THEN** the application obtains EID, Profiles, storage, and device information from one delegated snapshot call

#### Scenario: eSIM health follows overview

- **WHEN** eSIM health is requested while current device and eSIM snapshots are available
- **THEN** the health response is composed without repeating the full AT status and eSIM scans

### Requirement: Validated discovered AIDs SHALL remain generation-scoped

A successfully validated eUICC AID SHALL remain available for reads in the
current device generation. The service SHALL clear it only after reset,
reconnect, card identity change, or a validated target failure. Request
cancellation SHALL NOT invalidate the discovered target.

#### Scenario: Notification request is cancelled

- **WHEN** a notification read is cancelled while another valid discovered AID exists
- **THEN** the cancellation releases resources without clearing the discovered AID

#### Scenario: Discovered target fails validation

- **WHEN** the discovered AID cannot open or return a readable EID
- **THEN** the service clears it and performs at most one full static fallback scan

