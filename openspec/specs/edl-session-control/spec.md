# edl-session-control Specification

## Purpose
TBD - created by archiving change edl-live-session-control. Update Purpose after archive.
## Requirements
### Requirement: EDL status SHALL use live Sahara observation

When the selected physical device is in Qualcomm EDL, the service SHALL probe the active Sahara endpoint within a finite deadline and SHALL expose the observed protocol state and available device facts. It SHALL not use a normal-mode cache as the source of current EDL facts.

When no normal-mode candidate exists, the adapter SHALL accept one unique `05c6:9008` candidate. It SHALL reject zero or multiple candidates. It SHALL use the unique candidate physical location as the EDL-only session key.

The service SHALL NOT reuse a `detected` placeholder or a recovery observation as a successful live observation. It MAY reuse a recent `sahara_identified` or `firehose_ready` observation for a bounded interval. It SHALL retry live observation after that interval.

#### Scenario: EDL is detected with an empty normal-mode cache
- **WHEN** USB `05c6:9008` is found and Sahara observation succeeds
- **THEN** status reports the live Sahara state and observed facts without returning an unavailable firmware reason only because the cache is empty

#### Scenario: Sahara does not expose modem firmware revision
- **WHEN** Sahara returns HWID, serial, or SBL version but no verified modem revision
- **THEN** status keeps `firmware_revision` empty, reports each fact under `edl`, and states that AT firmware revision is not available in EDL

### Requirement: The service SHALL own one EDL session per physical device

The runtime SHALL retain one EDL session for a stable physical location across EDL re-enumeration. A session SHALL have a bounded renewable control lease and at most one active mutating operation.

The runtime SHALL retain no more than eight inactive physical-location sessions. It SHALL evict the oldest inactive session before it creates a ninth session. It SHALL not evict a session with a held lease or active operation.

An active operation SHALL pin its lease until the operation reaches a terminal state. A client SHALL NOT release or replace a pinned lease. Each browser tab SHALL keep its token in tab-local session storage.

#### Scenario: Two browsers request a mutation
- **WHEN** browser A holds the lease and browser B starts NAND backup or reset
- **THEN** browser B receives a structured session-conflict error and no device command starts

#### Scenario: Lease expires
- **WHEN** the lease owner stops renewing within the configured TTL
- **THEN** the runtime releases the lease after the bounded grace period and publishes the new session state

### Requirement: NAND backup SHALL not reset the device

After a valid NAND image is atomically published, the backup operation SHALL report success while the EDL session remains active. Reset and normal-mode reconnect SHALL be separate explicit operations.

#### Scenario: Valid image is read
- **WHEN** Firehose read and image validation succeed
- **THEN** the operation completes without calling Firehose reset and status remains in EDL

#### Scenario: Read fails or is cancelled
- **WHEN** the read fails or is cancelled after protocol setup
- **THEN** the service may attempt one bounded cleanup reset and reports whether recovery is still required

