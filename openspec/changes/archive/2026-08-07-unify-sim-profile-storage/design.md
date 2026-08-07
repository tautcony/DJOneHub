## Context

The application currently has two ICCID-keyed metadata models. `sim_cards` stores observed identity plus a local name, editable MSISDN, and notes; `app_settings.profile_notes` stores an in-memory JSON map of eSIM label, phone, and tags. Both are exposed independently and the eSIM editor writes the eUICC nickname and local metadata from one dialog. Existing databases can contain both records for the same ICCID with different values.

The active `redesign-esim-workbench` change already distinguishes the eUICC nickname from local metadata. This change moves the local side out of that workbench and makes the renamed SIM Profile registry its only owner.

## Goals / Non-Goals

**Goals:**

- Use one relational `sim_profiles` table and one application service for ICCID-keyed local metadata.
- Preserve existing physical SIM records and eSIM notes through a transactional schema migration.
- Separate device-observed identity from user-maintained metadata.
- Rename the HTTP resource and frontend domain consistently.
- Keep eSIM Profile metadata visible while limiting eSIM editing to the nickname stored on the eUICC.

**Non-Goals:**

- Merge the eUICC nickname into local metadata; they remain separate persistence targets.
- Normalize tags into a separate taxonomy table.
- Change Profile enable, disable, delete, download, notification, or operation behavior.
- Keep deprecated `/simcards` or `/esim/notes` routes.

## Decisions

### D1: Model physical SIMs and eSIM Profiles as SIM Profiles

`sim_profiles` is keyed by ICCID and contains observed fields (`imsi`, `msisdn`), local fields (`name`, `local_phone`, `notes`, `tags`), observation timestamps, and a `profile_type` value of `physical`, `esim`, or `unknown`. ICCID identifies the subscription Profile in both physical and eUICC form, so separate tables would recreate the ownership problem.

`msisdn` remains the modem-reported value. `local_phone` is never overwritten by observation and is the field edited by users. The UI may present the local phone first and the observed MSISDN as device information.

### D2: Rebuild the table transactionally in schema v6

Migration v6 creates `sim_profiles_v6`, copies `sim_cards`, imports and validates the `profile_notes` JSON document, swaps the relational table, recreates its index, records v6, and removes the settings namespace only in the same successful transaction. New databases create `sim_profiles` directly; earlier migration steps tolerate the absence of the legacy table.

For imported notes, non-empty values fill `name`, `local_phone`, and `tags`. When a legacy label conflicts with a non-empty SIM record name, the SIM record name remains canonical and the imported label is retained in `notes` as migration context rather than silently discarded. Profile-note phone does not conflict with observed `msisdn` because it maps to `local_phone`.

### D3: Make the registry service the only local metadata owner

The `simcards` package becomes `simprofiles`. It owns listing, creation, update, deletion, physical-SIM observation, and eSIM Profile observation. Storage exposes row-oriented methods only. `extras.Service` loses its ProfileNote map, lock, cache, and `ValueStore` dependency.

The eSIM application service registers all Profiles returned by a successful overview as `profile_type=esim`. Physical SIM polling registers the active ICCID as `profile_type=physical`; an existing explicit type is not downgraded to `unknown`.

### D4: Replace the API instead of retaining aliases

The server exposes `GET/POST /api/v1/sim-profiles` and `PUT/DELETE /api/v1/sim-profiles/{iccid}`. Requests and responses carry `name`, `local_phone`, `notes`, `tags`, observed identity, type, and timestamps. `/api/v1/simcards` and `/api/v1/esim/notes` are removed and OpenAPI is updated.

This is intentionally breaking: aliases would preserve two domain names and encourage new callers to continue using the obsolete ownership model.

### D5: Concentrate editing in SIM Profile Management

The navigation and route may retain the user-facing Chinese title “SIM 卡信息管理”, but frontend code, API methods, DTOs, and store ownership use SIM Profile terminology. The page edits all local fields.

The eSIM workbench joins its Profile snapshot with the SIM Profile store for display and search. Its editor contains only the eUICC nickname input; it does not call the SIM Profile update endpoint.

## Risks / Trade-offs

- **Breaking API consumers** → Update the bundled frontend, OpenAPI contract tests, and documentation in the same change; return normal not-found behavior for removed routes.
- **Migration conflict between two names** → Use a deterministic rule and retain the displaced label in notes; test real v5-to-v6 migration with conflicting data.
- **A read operation now records observed eSIM Profiles** → Keep observation idempotent and do not mutate user fields during an overview refresh.
- **Concurrent physical/eSIM observations** → Use ICCID upserts that update only observed fields and timestamps while preserving local metadata.
- **Dirty worktree overlaps the active eSIM redesign** → Apply focused edits against current content and retain all unrelated in-progress behavior.

## Migration Plan

1. Add migration and storage tests using real v5 schemas with SIM records, Profile notes, conflicts, and rollback injection.
2. Implement schema v6 and the `sim_profiles` storage API.
3. Replace application ownership and attach both physical and eSIM observers.
4. Replace HTTP routes and frontend clients, then move local editing to SIM Profile Management.
5. Remove obsolete ProfileNote code and update documentation.
6. Run Go tests, frontend lint/typecheck/build, and OpenSpec validation.

Rollback requires restoring a pre-v6 database backup because older binaries do not understand `sim_profiles`. The migration itself is atomic, so a failed upgrade leaves the v5 database usable.

## Open Questions

None. The user selected the `sim_profiles` name and requested the API and UI ownership changes in the same change.
