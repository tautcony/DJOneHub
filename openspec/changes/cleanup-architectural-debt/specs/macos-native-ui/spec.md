## ADDED Requirements

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
