## ADDED Requirements

### Requirement: SMS storage SHALL deduplicate per SIM identity and list within bounds

The SMS storage layer SHALL scope its deduplication uniqueness key to a non-empty stable SIM identity so an identical message received on a second SIM is stored rather than silently dropped, and SHALL support bounded listing with pagination instead of a full-table scan. If the SIM identity cannot be obtained, the caller SHALL retain the modem entry and retry instead of inserting under a shared empty identity.

#### Scenario: Same SMS arrives on two SIMs
- **WHEN** two SIMs receive the same SMS content within the deduplication window
- **THEN** both messages are stored because the uniqueness key includes the SIM identity

#### Scenario: Existing database upgrades to v3
- **WHEN** a v2 database containing the old table-level SMS uniqueness constraint is opened by the new version
- **THEN** migration v3 transactionally replaces the table and old constraint, preserves existing row IDs and data, and permits otherwise-identical messages with different SIM identities

#### Scenario: SIM identity is temporarily unavailable
- **WHEN** an inbound message is ready to persist but no stable SIM identity can be obtained
- **THEN** the message is not inserted under an empty identity and its modem entry remains available for retry

#### Scenario: Large SMS history is listed internally
- **WHEN** an application service lists SMS from storage with more rows than the bounded page size
- **THEN** storage returns a bounded page for the requested `limit`/`offset`, and the service can iterate pages without requiring a public HTTP pagination parameter

### Requirement: Status polling SHALL avoid redundant publication and probing

Polling services SHALL publish traffic events only when the observed traffic values change, and SHALL serve firmware status from a short-lived cache instead of re-running the full probe sequence (AT queries and ADB probing) on every read. Other status events are unchanged unless separately covered by an explicit event contract.

#### Scenario: Traffic sample is unchanged
- **WHEN** a polling cycle observes the same traffic values as the previous cycle
- **THEN** no traffic event is published for the unchanged sample

#### Scenario: Firmware status is read repeatedly
- **WHEN** firmware status is requested more than once within the cache lifetime
- **THEN** the second and later requests are served from the short-lived cache without repeating the probe sequence
