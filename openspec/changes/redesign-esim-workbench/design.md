## Context

The completed `complete-esim-management` change connected the missing core interactions: Profile Disable, staged download progress, dynamic confirmation-code replies, pending notification list/process/remove, notification history, and QR image input. The current implementation also retains Profile overview, Enable, Rename, Delete, local notes, and health data. Targeted verification performed while revising this design produced these results:

- `go test ./internal/application/esim ./internal/api/http ./internal/backend ./internal/esim` passes.
- `npm run typecheck`, `npm run lint`, and `npm run build` in `web/` pass.
- Explicit HTTP tests exist for Disable, notifications, confirmation-code round trip, notification history, and unknown confirmation operations.
- The frontend compiles and builds with its current QR and action wiring, but it has no equivalent component-level interaction suite.
- Verification coverage is uneven: several existing actions are implemented but need focused service/API acceptance tests before a redesign relies on them as proven behavior.

The problem is therefore primarily interaction architecture, not a missing feature surface. The current `EsimView.vue` combines Profile cards, pending notifications, history, download, confirmation input, and Profile settings in one long template. `useEsimStore` owns the domain data but does not model workspace navigation, filters, focused items, per-item notification state, or persistent presentation of an active operation.

MiniLPA remains a useful interaction reference for Profile-first organization, Profile/notification cross-navigation, search, drag/paste input, and card-operation progress. Its Chip and Behavior areas cannot be copied as active DJOneHub workspaces because DJOneHub currently has no verified public contract for rich eUICC details, default SM-DP+ editing, or configurable notification behavior.

Constraints:

- Only the existing eSIM route is redesigned; other routes and global navigation remain unchanged.
- Existing eSIM API contracts and operation/WebSocket protocol remain stable.
- The redesign may add client-side organization and input affordances, but it may not manufacture backend capabilities.
- Card writes continue to serialize through the existing runtime/application behavior.
- Sensitive identifiers remain controlled by the existing privacy setting.

## Goals / Non-Goals

**Goals:**

- Prove the existing eSIM contracts used by the new UI before replacing the old presentation.
- Make Profile management and notification handling discoverable, connected, and efficient.
- Keep download input, progress, and dynamic confirmation in one continuous interaction.
- Keep accepted operations visible until their actual terminal state and refreshed snapshot converge.
- Improve loading, empty, unavailable, error, destructive, responsive, and keyboard states.

**Non-Goals:**

- Adding new eSIM device capabilities or public endpoints as part of the redesign.
- Rich eUICC chip information, default SM-DP+ editing, behavior-policy persistence, notification batching, SM-DS discovery, eUICC reset, camera scanning, or IMEI override.
- Redesigning Overview, SMS, Network, Settings, or the application shell.
- Replacing Vue, Pinia, Ant Design Vue, typed ViewContext, `jsqr`, or operation/WebSocket infrastructure.

## Decisions

### D1: Add a capability verification gate before UI replacement

Implementation starts by adding focused service/API tests for every existing action the redesigned page will expose. Tests must verify more than route existence: synchronous actions must mutate or return the expected state; asynchronous actions must return an operation ID and reach the expected terminal/event behavior; notification actions must refresh pending/history semantics; confirmation replies must resume or cancel the correct download.

The initial evidence matrix is:

| Capability | Current implementation | Current evidence | Required before redesign uses it |
| --- | --- | --- | --- |
| Overview/EID/Profile list | service + `GET /esim` | package suite passes | focused response/state test |
| Enable | service + action API + UI | package suite passes | focused operation/event test |
| Disable | service + action API + UI | explicit HTTP test | retain and extend terminal-state assertion |
| Rename | service + action API + editor | package suite passes | focused rename and error test |
| Delete | service + action API + UI | package suite passes | focused operation/event and enabled-state guard test |
| Local notes | `/esim/notes` + store | package suite passes | focused read/write and mixed rename/note failure test |
| Download | service + action API | activation parsing and confirmation round-trip tests | staged progress, cancellation, and terminal refresh test |
| Pending notifications | list/process/remove APIs | explicit HTTP test | per-command failure/history transition tests |
| Notification history | history API + SQLite | explicit endpoint/storage tests | retain ordering/state assertions |
| QR image input | `jsqr` file/paste path | typecheck/lint/build | browser interaction checks with valid/invalid fixtures |
| Health | existing endpoint + loaded store state | package suite passes | focused stable-shape/failure-isolation test before display |

If verification exposes a defect, fixing that defect is permitted because it makes an already committed capability effective. Adding a new endpoint or device behavior is not permitted without a separate scope decision.

### D2: Use two page-local workspaces, not four feature tabs

`EsimView` becomes a workbench shell with a shared compact summary and two tabs: Profiles and Notifications. Profiles is default; Notifications carries the pending count and contains Pending/History segmented views. Existing EID, active Profile/Profile count, health when verified, refresh, and download actions belong in the summary.

This reflects the effective product surface. The previous proposal's eUICC Information and Behavior tabs are removed because their intended controls required new APIs. A four-tab shell would create empty or misleading areas and shift effort away from interaction quality.

### D3: Split rendering into focused components while retaining one domain store

Create focused components under `web/src/components/esim/`:

- `EsimSummaryBar`
- `ProfileWorkspace`, `ProfileCard`, and `ProfileEditor`
- `NotificationWorkspace`, `PendingNotifications`, and `NotificationHistory`
- `ProfileDownloadFlow`
- `EsimOperationDock`

`EsimView` owns composition. `useEsimStore` remains the canonical domain owner and gains bounded UI state: active workspace, pending/history mode, search/filter values, focused Profile/ICCID, active notification action, download presentation state, and current operation presentation. Computed values perform all search/filter behavior locally over current snapshots.

Multiple stores were rejected because Profile operations, notifications, health, and the active operation must refresh together. Keeping the current monolithic template was rejected because it obscures state ownership and makes responsive behavior difficult to verify.

### D4: Keep the API surface unchanged

The redesigned frontend reuses:

```text
GET    /api/v1/esim
GET    /api/v1/esim/health
GET    /api/v1/esim/notes
PUT    /api/v1/esim/notes
POST   /api/v1/esim/actions/download
POST   /api/v1/esim/actions/enable
POST   /api/v1/esim/actions/disable
POST   /api/v1/esim/actions/rename
POST   /api/v1/esim/actions/delete
GET    /api/v1/esim/notifications
GET    /api/v1/esim/notifications/history
POST   /api/v1/esim/notifications/{sequence}/process
DELETE /api/v1/esim/notifications/{sequence}
POST   /api/v1/esim/operations/{operation_id}/confirmation-code
GET    /api/v1/operations/{operation_id}
```

No batch, preference, rich detail, or default-address endpoint is added. Notification controls remain single-item; local filtering and cross-navigation improve efficiency without changing remote semantics.

### D5: Present Profiles with one clear state action and contextual secondary actions

Profile cards show nickname, provider, enabled/disabled state, masked ICCID, class, and local metadata. Search covers nickname, provider, full in-memory ICCID, and local tags; display remains masked. The valid state transition is prominent: Enable for disabled, Disable for enabled. Rename/local metadata, related notifications, and Delete move to a contextual menu or detail action area. Delete is unavailable for enabled Profiles and always target-confirmed.

MiniLPA's double-click enable and focus-dependent toolbar commands are not adopted because they are hidden interactions and unsuitable for touch or keyboard users.

### D6: Separate Pending and History while preserving single-item notification semantics

Notifications uses a segmented Pending/History control. Pending supports local text, event, and Profile filters plus existing Process and Remove actions. History supports local state/event/Profile/text filters and no write actions. Profile-to-notification and notification-to-Profile links update the active tab and ICCID filter.

Per-entry action state replaces the current global `notificationBusy` presentation where feasible so processing one row does not make unrelated rows appear to be the target. The backend remains the authority and pending/history snapshots reload after completion. Batch selection is intentionally excluded until a batch contract exists.

### D7: Consolidate download presentation without changing download protocol

`ProfileDownloadFlow` combines:

1. activation code input and advanced optional fields;
2. QR file selection, existing clipboard image decode, and drag/drop through the same `jsqr` helper;
3. submission to the existing download endpoint;
4. progress for the returned operation ID;
5. dynamic confirmation-code input for a matching request event;
6. success, failure, or cancellation with a retry path.

The backend remains authoritative for `LPA:1$...` parsing and matching ID resolution. The frontend may show a lightweight, masked summary but does not introduce a preview endpoint or infer server requirements. The separate confirmation modal is removed only after the integrated flow passes the existing round-trip behavior.

### D8: Keep an operation dock independent of transient dialogs

After Enable, Disable, Delete, or Download is accepted, an `EsimOperationDock` displays operation name, target where safe, progress, message, and terminal result. Request acceptance is not shown as device success. Conflicting card-write controls are disabled based on active operation state. Terminal success triggers coordinated reload of overview, notes/health as applicable, pending notifications, and history.

Closing the download dialog hides only its expanded presentation; it does not clear the operation ID. Existing operation status resynchronization handles WebSocket interruption. This avoids the current pattern where the download modal closes immediately after acceptance and leaves progress at the bottom of a long Profile panel.

### D9: Treat unavailable, partial failure, privacy, and responsive behavior as first-class states

Overview failure blocks Profile operations; optional notes or health failure degrades only those fields; notification load failure remains visible rather than being indistinguishable from an empty list. Card type `unknown`, readable empty eUICC, loading, no search matches, and no pending notifications each receive distinct states.

All identifiers follow `maskSensitive`; unmasked values do not enter visible labels, tooltips, route parameters, or operation text while privacy is disabled. Layout uses a bounded summary grid, responsive Profile grid, stacked notification actions on narrow viewports, viewport-bounded dialogs, and accessible names/tooltips for icon-only controls.

## Risks / Trade-offs

- [Existing code passes package tests but an individual behavior may still be ineffective] -> Complete the capability verification gate before replacing its UI and fix only defects within the already committed contract.
- [Frontend interaction coverage is currently build-only] -> Add deterministic QR fixtures and browser interaction checks for every primary workflow and state before removing the old presentation.
- [A single operation dock can hide multiple concurrent notification actions] -> Card writes remain one active operation; notification commands retain per-entry busy/error state outside the dock.
- [Health is currently untyped and optional] -> Define a frontend type only after verifying the existing response shape, and isolate health failure from Profile availability.
- [Cross-navigation filters can make lists appear empty] -> Display active filter context prominently and provide one-command clearing.
- [Refactoring a working page can regress action wiring] -> Preserve store/API methods initially, move one workflow at a time, and run contract plus browser checks before deleting old markup.

## Migration Plan

1. Add and run the capability verification suite against the current page contracts; resolve blockers without expanding product scope.
2. Add bounded interaction state and computed filters to `useEsimStore` while keeping existing API methods unchanged.
3. Build the summary and Profiles workspace, then verify every existing Profile action before removing its old markup.
4. Build Pending/History notification views and cross-navigation, then verify single-item process/remove and history transitions.
5. Integrate download input, progress, and confirmation into one presentation, then verify manual, QR, failure, confirmation, cancellation, and reconnect paths.
6. Complete operation dock, error/empty states, responsive styling, localization, and keyboard behavior.
7. Run all backend/frontend checks and demo browser scenarios before marking the change complete.

Rollback is frontend-local: the previous `EsimView` can be restored because no API or database migration is introduced. Any contract test or defect fix added during the verification gate remains useful and does not prevent rollback.

## Open Questions

- Determine whether the current health response is stable enough to display in the shared summary; omit it if focused contract verification fails.
- Determine whether the current operation manager can expose more than one relevant eSIM operation at a time; if not, explicitly constrain the dock to the current active card operation.
