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
