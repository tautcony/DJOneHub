# macos-native-ui Specification

## Purpose

The native macOS notification experience — the DJOneHubNotifier app that surfaces calls, SMS, and eSIM prompts in the system UI — SHALL interact with the Go backend through a well-defined bridge with strict actor-isolation and threading guarantees. Notifications SHALL respect user privacy preferences (message content suppression), SHALL be marked time-sensitive for incoming calls, and SHALL never block backend event consumption. The Go side SHALL verify the bridge threading contract instead of assuming the main actor.
## Requirements
### Requirement: macOS notification callbacks SHALL NOT violate actor isolation

The native notification delegate SHALL handle `UNUserNotificationCenter` callbacks without Swift 6 main-actor isolation violations, SHALL invoke the completion handler within the callback, and SHALL perform state access on the main actor.

#### Scenario: Notification is clicked at cold start
- **WHEN** the app is launched from a notification click and the delegate callback arrives on a background queue
- **THEN** the process does not abort, the completion handler is invoked, and the panel opens

#### Scenario: Notification is presented while the app runs
- **WHEN** a notification is presented while the app is already running
- **THEN** the delegate completes without an isolation violation and the notification is shown or suppressed per configuration

### Requirement: Native UI notification delivery SHALL be decoupled from event consumption

The notification service SHALL deliver approved prompts and menu-bar updates to the native UI through an internal queue with a dedicated delivery goroutine so a slow or blocked native bridge never blocks event consumption, and SHALL reconcile call and SMS state from the application services after delivery recovers so user-facing state converges.

#### Scenario: Bridge is slow
- **WHEN** the native bridge is slow or blocked while call or SMS events arrive
- **THEN** the notification consumer keeps processing events and the prompts are queued and delivered once the bridge recovers

#### Scenario: State is reconciled after recovery
- **WHEN** delivery recovers after events were dropped
- **THEN** the active call, missed call, and recent SMS state are re-derived from the extras and SMS services so the UI does not remain stuck, such as a call card persisting after the call ended

### Requirement: Swift-to-Go bridge commands SHALL report queue rejection

The Swift-to-Go command queue SHALL report a command that cannot be enqueued through a Go-to-Swift `command.dropped` event and a Go diagnostic entry instead of silently discarding it. The feedback SHALL identify the command and reason; the native UI SHALL clear or re-enable any pending user action according to that command's recovery contract.

#### Scenario: Command queue is full
- **WHEN** the Swift-to-Go command queue is full and a user command such as reject-call cannot be enqueued
- **THEN** Go emits a `command.dropped` feedback event naming the command and reason, records the drop diagnostically, and does not claim that the command executed

#### Scenario: Reject-call enqueue is rejected
- **WHEN** Swift receives `command.dropped` for a reject-call action
- **THEN** it clears the pending rejecting state, restores the actionable buttons, and allows the user to retry

### Requirement: Reject-call state SHALL recover within a bounded time

The native UI SHALL clear the in-progress reject state after a bounded timeout when the reject command was lost or the device did not respond, and SHALL restore the actionable buttons so the user can retry.

#### Scenario: Reject command is lost
- **WHEN** a reject-call command is lost or the device does not respond within the timeout
- **THEN** the UI clears the rejecting state, re-enables the buttons, and the user can retry

### Requirement: Native notifications SHALL suppress message content per preference

The native UI SHALL provide a preference that shows only the sender of an SMS notification; when the preference is enabled, the notification request SHALL NOT include the message body, so the body is absent from the banner, lock screen, and notification center where the system displays the request. The preference SHALL default to enabled when the persisted field is absent, while an explicit disabled value remains respected.

#### Scenario: Sender-only preference is enabled
- **WHEN** an SMS notification is delivered while the sender-only preference is enabled
- **THEN** the notification shows the sender and no message body, even for content such as one-time verification codes

#### Scenario: Sender-only preference is disabled
- **WHEN** an SMS notification is delivered while the sender-only preference is disabled
- **THEN** the notification includes the message body as before

#### Scenario: Persisted preference has no sender-only field
- **WHEN** existing preferences are loaded without a `sender_only` field
- **THEN** presence-aware normalization enables sender-only mode rather than treating the missing field as an explicit opt-out

### Requirement: Time-sensitive notifications SHALL be marked appropriately

Incoming call notification requests SHALL use the time-sensitive interruption level. Final presentation remains subject to the user's Focus and notification authorization settings.

#### Scenario: Call arrives during Focus
- **WHEN** an incoming call notification is posted while the user's Focus mode is active
- **THEN** the notification request uses the time-sensitive interruption level; the system may still suppress presentation according to user authorization and Focus policy

### Requirement: The native UI SHALL verify the bridge threading contract

The native UI SHALL check the Go-to-Swift bridge threading contract explicitly instead of assuming main-thread execution, and SHALL surface a readable error when the contract is violated. The contract SHALL be documented in the bridge header.

#### Scenario: Bridge callback arrives on the wrong thread
- **WHEN** a bridge event arrives on a thread other than the one guaranteed by the contract
- **THEN** the native UI reports a readable threading error instead of trapping with no diagnostics

