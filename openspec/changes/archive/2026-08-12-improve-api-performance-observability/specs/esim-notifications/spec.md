## ADDED Requirements

### Requirement: Pending notification reads SHALL use the reusable snapshot

The eSIM service SHALL use one successful pending-notification snapshot for five
seconds per runtime generation and SHALL coalesce concurrent cold reads. It
SHALL invalidate the snapshot after notification process or removal, eSIM
Profile mutation, reset, card change, or validated target failure.

#### Scenario: Two clients list pending notifications
- **WHEN** both requests arrive before one card read completes
- **THEN** one shared load runs and both active callers receive its result

#### Scenario: Runtime generation changes
- **WHEN** the device reconnects while a snapshot is within its TTL
- **THEN** the old snapshot is not returned for the new generation
