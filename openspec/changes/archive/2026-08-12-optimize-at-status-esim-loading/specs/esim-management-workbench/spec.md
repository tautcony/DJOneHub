## ADDED Requirements

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
