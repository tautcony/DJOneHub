## ADDED Requirements

### Requirement: eSIM reads SHALL use validated discovered targets

The eSIM service SHALL prefer eUICC AIDs discovered in the current device generation, SHALL validate them through a readable card session, and SHALL perform at most one full static discovery fallback when all preferred targets fail. Modem or SIM reset invalidation SHALL prevent stale targets from being reused without validation, and every LPA client SHALL remain scoped to one operation and be closed afterward.

#### Scenario: Known AID remains valid
- **WHEN** a repeated eSIM read starts with a previously discovered AID that opens and returns its EID
- **THEN** the service completes the read without probing unrelated static candidate AIDs and closes the operation's LPA client

#### Scenario: Known AID is stale
- **WHEN** every previously discovered AID fails to open or return a readable EID
- **THEN** the service clears the stale fast path, performs one full static discovery scan, and uses the newly validated target

#### Scenario: Reset invalidates discovery
- **WHEN** the modem or SIM reset boundary is observed
- **THEN** subsequent eSIM reads do not assume the pre-reset target remains valid and rediscover as required

### Requirement: Public eSIM overview SHALL use a lightweight Profile snapshot

The application eSIM overview SHALL obtain only the EID, basic Profile fields required by the stable response, and the live active ICCID tie-breaker. It SHALL NOT require rich eUICC information, configured addresses, certificates, manufacturer lookup, or product-AID fields to return the public Profile overview.

#### Scenario: Client requests Profile overview
- **WHEN** the application handles the public eSIM overview use case
- **THEN** it returns the existing EID and Profile response without issuing rich chip or product-information reads

#### Scenario: Rich details are requested internally
- **WHEN** an internal caller explicitly requests the rich eUICC overview
- **THEN** the manager may perform enrichment without treating a lightweight Profile snapshot as complete rich data

### Requirement: eSIM reads SHALL expose actionable latency evidence

The eSIM manager SHALL log the AID selection policy, fallback occurrence, and aggregate elapsed time for Profile and notification card reads while avoiding unbounded per-APDU success logging.

#### Scenario: Fast Profile read completes
- **WHEN** a Profile snapshot is loaded from a discovered target
- **THEN** structured diagnostics identify the fast-path policy and total read duration

#### Scenario: Discovery fallback occurs
- **WHEN** the preferred target fails and a full scan is attempted
- **THEN** structured diagnostics record that fallback occurred without exposing Profile credentials or activation data
