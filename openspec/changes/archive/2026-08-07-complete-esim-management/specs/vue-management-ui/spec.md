## ADDED Requirements

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
