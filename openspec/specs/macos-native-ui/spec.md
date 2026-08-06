# macos-native-ui Specification

## Purpose
TBD - created by archiving change fix-security-and-data-loss. Update Purpose after archive.
## Requirements
### Requirement: macOS notification callbacks SHALL NOT violate actor isolation

The native notification delegate SHALL handle `UNUserNotificationCenter` callbacks without Swift 6 main-actor isolation violations, SHALL invoke the completion handler within the callback, and SHALL perform state access on the main actor.

#### Scenario: Notification is clicked at cold start
- **WHEN** the app is launched from a notification click and the delegate callback arrives on a background queue
- **THEN** the process does not abort, the completion handler is invoked, and the panel opens

#### Scenario: Notification is presented while the app runs
- **WHEN** a notification is presented while the app is already running
- **THEN** the delegate completes without an isolation violation and the notification is shown or suppressed per configuration

