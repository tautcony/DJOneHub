## Purpose

Define the eUICC notification management contract for the eSIM service: listing pending notifications, retrying delivery to the SM-DP+, and removing notifications without retry.

## Requirements

### Requirement: The eSIM service SHALL expose pending eUICC notifications

The eSIM service SHALL list pending eUICC notifications, retry sending a notification to its SM-DP+, and remove a notification from the eUICC list. Retrying SHALL use the existing `HandleNotification`/`RemoveNotificationFromList` path, SHALL only remove a notification after a successful send, and SHALL report per-notification failure without aborting the remaining list.

#### Scenario: Pending notifications are listed
- **WHEN** the user requests the eUICC notification list
- **THEN** the service returns each pending notification's sequence number, event type, and profile identifier, or an empty list when none are pending

#### Scenario: A notification is retried successfully
- **WHEN** the user retries a pending notification and the SM-DP+ accepts it
- **THEN** the service sends the notification, removes it from the eUICC list, and reports success

#### Scenario: A notification retry fails
- **WHEN** the user retries a pending notification and the SM-DP+ rejects or cannot be reached
- **THEN** the service returns a structured error, keeps the notification in the eUICC list, and does not abort other pending notifications

#### Scenario: A notification is removed without retry
- **WHEN** the user removes a pending notification directly
- **THEN** the service deletes it from the eUICC list and reports the removal

### Requirement: Notification listing SHALL target discovered eUICCs

Pending notification listing without an explicit AID SHALL read each distinct eUICC target discovered in the current device generation and SHALL NOT use the complete static compatibility AID table as the notification target list. If no target is available or all preferred targets fail, the service SHALL perform discovery fallback and retain support for devices with multiple distinct eUICCs.

#### Scenario: One eUICC has multiple compatible AIDs
- **WHEN** discovery identifies one EID through a validated primary AID and other static aliases could also open the same card
- **THEN** notification listing reads that eUICC once through the discovered target rather than querying every alias

#### Scenario: eSTK Max exposes two eUICCs
- **WHEN** discovery identifies distinct EIDs for SE0 and SE1
- **THEN** notification listing reads both discovered targets and combines their pending notifications

#### Scenario: Notification target becomes stale
- **WHEN** all discovered notification targets fail to open or return notification data
- **THEN** the service performs one discovery fallback and either returns the recovered notification snapshot or the structured read error
