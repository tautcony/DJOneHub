## ADDED Requirements

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
