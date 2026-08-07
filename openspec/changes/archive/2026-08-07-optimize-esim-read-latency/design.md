## Context

`Manager` already retains `discoveredEUICCs` and an overview cache, but `getEffectiveAIDPlan` always returns the full static candidate list. A cold public overview calls the rich `GetEsimOverview` path even though the application response only needs one EID and basic Profiles. Pending notification listing independently opens every static candidate AID. The frontend then waits for overview, health, and notifications in sequence.

Logical channels cannot be treated as durable state: modem reset, SIM authentication, and Profile switching can invalidate them. The optimization must therefore retain discovery knowledge and snapshots, not a permanently open LPA client. Existing public HTTP response shapes and the active `redesign-esim-workbench` change remain constraints.

## Goals / Non-Goals

**Goals:**

- Make repeated eSIM reads prefer previously validated eUICC AIDs and recover automatically when that knowledge is stale.
- Remove rich chip/product APDUs from the public Profile overview path.
- Avoid static-candidate notification scans while preserving multi-eUICC behavior.
- Render valid Profile data without waiting for auxiliary health or notification reads.
- Preserve cancellation, reset invalidation, and operation-scoped logical-channel cleanup.
- Add enough stage timing to distinguish discovery, EID, Profile, notification, and product-info costs.

**Non-Goals:**

- Permanently retaining an LPA client or logical channel.
- Adding or changing public eSIM endpoints or response fields.
- Persisting eUICC discovery across process restarts.
- Running APDU sessions concurrently on the same modem.
- Redesigning the eSIM workbench presentation.

## Decisions

### D1: Treat discovered eUICCs as a validated fast path

`getEffectiveAIDPlan` will put normalized AIDs from `discoveredEUICCs` first and identify the plan as a discovered fast path. The scan will try those targets only. If no target yields a readable EID, it will retry once with the full static candidate list and replace discovery with the successful results.

This chooses optimistic validation over blindly trusting cached AIDs. Keeping the existing full scan for every request was rejected because it makes discovery state useless. Persisting AIDs was rejected because the process does not yet have a reliable card-generation identity across restarts.

For eSTK Max, separately discovered SE0 and SE1 targets remain in the plan. Multiple AIDs that return the same EID are deduplicated by EID during a scan.

### D2: Keep LPA sessions operation-scoped

The manager continues to create and close an LPA client around each read or command. Discovery caching removes repeated candidate selection, while existing cleanup and reset handling retain ownership of logical-channel lifecycle.

A permanent client was rejected because channel validity cannot be inferred after modem reset, SIM authentication, or a Profile switch. An idle client pool is deferred until timing evidence shows channel open/close is still dominant after the safer optimizations.

### D3: Add a lightweight Profile snapshot loader

The public `ESIMPort.EID` and `Profiles` methods will share a cached manager snapshot populated by EID plus `listBasicProfiles`. It will not call `EUICCInfo2`, configured-address, certificate/manufacturer, or eSTK Product AID enrichment. Rich `GetEsimOverview` remains available to internal callers that explicitly need those fields.

The lightweight and rich caches remain separate so a Profile request cannot incorrectly satisfy a rich-detail request. Both use singleflight. Reset and Profile mutations invalidate the Profile snapshot; rename may patch or invalidate it consistently with the existing overview behavior.

### D4: Notification listing uses resolved eUICC targets

When no explicit AID is supplied, notification listing uses discovered targets. If none exist, it performs discovery before listing. If all discovered targets fail, it performs one full discovery fallback and retries. It never treats the full static AID table as a notification target list.

Automatic notification cleanup remains in scope for compatibility, but the read must avoid duplicate aliases for the same discovered EID. Moving cleanup to a background worker is deferred because it changes observable pending/history timing beyond this latency fix.

### D5: Make frontend loading progressive without unsafe APDU concurrency

`useEsimStore.load` will expose overview as soon as it resolves. Local notes load independently because they are SQLite-backed. Health begins after overview but is not awaited by the view loader. Notifications are also scheduled without gating `markViewLoaded`; the workbench can still explicitly refresh them.

This avoids making overview and notification APDU sessions concurrently mandatory. The existing manager serialization remains authoritative if background requests overlap.

### D6: Record stage-level slow-read evidence

Structured logs will include the selected AID policy, candidate count, fallback occurrence, and elapsed milliseconds for lightweight Profile and notification reads. Per-APDU logging remains thresholded to avoid flooding normal logs.

## Risks / Trade-offs

- [A discovered AID becomes stale after an unobserved card change] -> Validate it by opening the AID and reading EID, then perform one full fallback scan.
- [Fast-path fallback duplicates work on the first failed request] -> Limit fallback to one attempt and clear stale discovery before retrying.
- [Lightweight and rich caches diverge] -> Invalidate both at existing reset/mutation boundaries and test independent cache ownership.
- [Background health/notification requests overlap card reads] -> Do not require concurrency for correctness; retain manager/APDU arbitration and make Profile rendering independent of auxiliary completion.
- [Existing tests encode full-static behavior] -> Replace them with explicit fast-path, fallback, multi-eUICC, and invalidation tests.
- [Notification auto-cleanup still dominates latency when retries occur] -> Add stage timing now and treat asynchronous cleanup as a follow-up only if measurements justify its behavior change.

## Migration Plan

1. Add fast-path planning and fallback tests, then update manager discovery selection.
2. Add the lightweight Profile snapshot and switch the AT eSIM port to it without changing HTTP DTOs.
3. Route notification listing through discovered targets and verify single/multi-eUICC behavior.
4. Make frontend auxiliary reads non-blocking and verify load/error isolation.
5. Run focused manager/application/API tests, the full Go suite, and frontend typecheck/lint/build.

Rollback is code-only: restore full-static planning and the rich overview-backed port methods. There is no schema, persisted-state, or public-contract migration.

## Open Questions

- Hardware measurements after implementation will determine whether notification auto-cleanup needs a separate asynchronous design.
- Cross-process persistence of the last valid AID may be considered later if cold process startup remains material after lightweight Profile reads.
