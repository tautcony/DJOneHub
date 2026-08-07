## 1. Storage Schema and Migration

- [x] 1.1 Add focused v5-to-v6 migration tests for existing SIM records, Profile notes, conflicting names, namespace removal, fresh databases, and transactional rollback.
- [x] 1.2 Replace the baseline `sim_cards` schema with `sim_profiles`, including observed identity, local metadata, Profile type, timestamps, and indexes.
- [x] 1.3 Implement transactional migration v6 from `sim_cards` and `app_settings.profile_notes` with deterministic conflict preservation.
- [x] 1.4 Replace SimCard storage records and CRUD helpers with validated SimProfile row operations and idempotent physical/eSIM observation upserts.

## 2. Backend Ownership and API

- [x] 2.1 Rename the SIM card application package and service contracts to SIM Profiles and support name, local phone, notes, tags, type, and observed fields.
- [x] 2.2 Register installed Profiles from successful eSIM overview reads without overwriting local metadata.
- [x] 2.3 Remove ProfileNote persistence and cache ownership from `extras.Service` and application assembly.
- [x] 2.4 Replace `/api/v1/simcards` with `/api/v1/sim-profiles`, remove `/api/v1/esim/notes`, and update OpenAPI declarations.
- [x] 2.5 Update storage, application, and HTTP tests for the unified contracts and removed routes.

## 3. Frontend Ownership

- [x] 3.1 Rename frontend DTOs, API methods, store ownership, and context fields to SIM Profiles and use `/sim-profiles`.
- [x] 3.2 Extend SIM Profile Management to create and edit local name, phone, notes, and tags while showing observed IMSI/MSISDN separately.
- [x] 3.3 Load SIM Profile metadata for eSIM display and search, remove `/esim/notes` state/actions, and retain only eUICC nickname editing in the eSIM editor.
- [x] 3.4 Update Chinese and English copy, empty states, labels, and responsive layout for the unified model.

## 4. Documentation and Verification

- [x] 4.1 Update storage, README, and relevant eSIM documentation to describe `sim_profiles` and the new ownership/API boundary.
- [x] 4.2 Run focused Go tests, full Go tests, frontend format/lint/typecheck/build, and OpenSpec validation; resolve all regressions in scope.
