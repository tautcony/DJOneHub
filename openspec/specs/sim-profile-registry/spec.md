## Purpose

Define the canonical ICCID-keyed registry for physical SIMs and eSIM Profiles, including local metadata ownership, device observations, migration, and CRUD identity rules.

## Requirements

### Requirement: Local SIM Profile metadata SHALL have one relational owner

The application SHALL store physical SIM and eSIM Profile records in one `sim_profiles` table keyed by normalized ICCID. Device-observed IMSI and MSISDN SHALL remain separate from user-maintained name, local phone, notes, and tags, and observation SHALL NOT overwrite user-maintained fields.

#### Scenario: Physical SIM is observed
- **WHEN** device polling reports a non-empty ICCID with IMSI or MSISDN
- **THEN** the registry creates or updates that SIM Profile's observed fields and timestamps without changing its local metadata

#### Scenario: Installed eSIM Profile is observed
- **WHEN** a successful eSIM overview returns an installed Profile with an ICCID
- **THEN** the registry creates or updates an eSIM SIM Profile without requiring that Profile to become active

#### Scenario: User edits local metadata
- **WHEN** the user saves a name, local phone, notes, or tags for an ICCID
- **THEN** the registry persists those fields in the same `sim_profiles` row and later device observations preserve them

### Requirement: Existing metadata SHALL migrate atomically to sim_profiles

Schema migration v6 SHALL transactionally preserve existing `sim_cards` rows and import `app_settings.profile_notes` into `sim_profiles`. It SHALL remove the obsolete settings namespace only when the table migration and metadata import both succeed.

#### Scenario: Existing v5 database upgrades
- **WHEN** a v5 database contains SIM card rows and Profile notes for distinct or matching ICCIDs
- **THEN** opening it produces one `sim_profiles` row per ICCID with observed and local fields migrated according to the defined conflict rule

#### Scenario: Migration fails before commit
- **WHEN** an injected error interrupts the v6 rebuild or Profile-note import
- **THEN** the transaction rolls back, schema version 6 is not recorded, and the legacy table and settings document remain intact

#### Scenario: New database is created
- **WHEN** the application starts without an existing database
- **THEN** it creates `sim_profiles` directly and does not create `sim_cards` or a `profile_notes` settings document

### Requirement: SIM Profile CRUD SHALL validate stable identity

The registry SHALL require a non-empty ICCID no longer than 22 characters for create and update operations, SHALL keep ICCID immutable, and SHALL return explicit conflict and not-found errors for duplicate creation and missing targets.

#### Scenario: Duplicate Profile is manually created
- **WHEN** a user creates a SIM Profile whose ICCID already exists
- **THEN** the service returns an operation-conflict error without replacing the existing record

#### Scenario: Missing Profile is updated
- **WHEN** a user updates local metadata for an ICCID that is not present
- **THEN** the service returns a not-found error instead of reporting a successful no-op
