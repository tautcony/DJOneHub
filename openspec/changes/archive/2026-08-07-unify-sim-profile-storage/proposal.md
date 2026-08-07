## Why

DJOneHub currently stores ICCID-keyed local metadata twice: SIM card records live in the relational `sim_cards` table while eSIM Profile notes live as one JSON document under `app_settings.profile_notes`. The duplicate ownership allows the same subscription to acquire conflicting names and phone values and incorrectly treats domain data as application settings.

## What Changes

- Replace `sim_cards` with one canonical `sim_profiles` table for physical SIMs and eSIM Profiles, keyed by ICCID.
- Migrate existing SIM records and `app_settings.profile_notes` data transactionally without losing either source.
- Store device-observed IMSI/MSISDN separately from user-maintained name, phone, notes, and tags.
- Move all local metadata creation and editing to SIM Profile Management; installed eSIM Profiles are registered there when observed.
- Keep the eSIM workbench read-only for local metadata and retain only the eUICC Profile nickname editor as a card write.
- **BREAKING**: replace `/api/v1/simcards` with `/api/v1/sim-profiles` and update its DTO to expose the unified profile metadata.
- **BREAKING**: remove `/api/v1/esim/notes`; eSIM presentation obtains local metadata from the SIM Profile registry.

## Capabilities

### New Capabilities

- `sim-profile-registry`: Defines the ICCID-keyed relational registry, observation behavior, metadata ownership, migration, and SIM Profile API.

### Modified Capabilities

- `device-api`: Replaces the SIM card and eSIM-note endpoints with the unified SIM Profile API.
- `vue-management-ui`: Moves local metadata editing to SIM Profile Management and limits eSIM editing to the eUICC nickname.

## Impact

- Storage schema and migration logic in `internal/storage/sqlite.go`, including removal of the `profile_notes` settings document after successful import.
- Application ownership in `internal/application/simcards`, `internal/application/esim`, and `internal/application/extras`, plus app assembly and HTTP routing.
- Frontend API types, stores, SIM Profile management UI, eSIM store/view, navigation copy, and tests.
- Existing installations upgrade automatically; API clients using `/simcards` or `/esim/notes` must move to `/sim-profiles`.
