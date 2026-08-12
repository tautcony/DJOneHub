## ADDED Requirements

### Requirement: Live device status SHALL use bounded snapshots

Identity, radio, and SIM status reads SHALL use short-lived, generation-scoped
snapshots. Concurrent callers for the same uncached status SHALL share one
refresh. A device generation change SHALL invalidate the snapshots. Failed
reads SHALL NOT replace a valid snapshot.

#### Scenario: Status is read repeatedly

- **WHEN** device status is requested more than once within the status TTL
- **THEN** later requests use the cached identity, radio, and SIM values without repeating their AT commands

#### Scenario: Device reconnects

- **WHEN** the runtime device generation changes
- **THEN** the next status request performs a fresh backend read instead of using the previous generation snapshot

### Requirement: Ordinary SIM status SHALL NOT discover EID

The ordinary SIM status surface SHALL report SIM insertion state, IMSI, and
ICCID without invoking eSIM discovery. EID SHALL be obtained only through the
eSIM service surface.

#### Scenario: SIM status is read for an eSIM card

- **WHEN** a caller requests ordinary SIM status
- **THEN** the backend does not issue eUICC AID discovery or EID APDUs
