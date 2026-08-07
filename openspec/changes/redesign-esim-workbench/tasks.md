## 1. Verify Existing Capability Contracts

- [ ] 1.1 Add focused overview tests covering readable eUICC, empty Profile list, unknown/unreadable card, EID, Profile state, and request cancellation.
- [ ] 1.2 Add focused Enable, Disable, and Delete service/API tests that assert operation ID, progress/terminal state, `esim.updated`, and target ICCID behavior.
- [ ] 1.3 Add focused Rename tests covering success, validation, backend failure, event publication, and unchanged input behavior.
- [ ] 1.4 Add focused local-note read/write tests and define expected behavior when rename and local-note persistence do not both succeed.
- [ ] 1.5 Extend download tests to assert activation-code resolution, staged progress forwarding, success, backend failure, confirmation submit, decline, timeout/cancellation, and operation cleanup.
- [ ] 1.6 Extend pending notification tests for list, retryability, process success/failure, remove success/failure, and the resulting notification-history state transitions.
- [ ] 1.7 Verify notification-history ordering, bounds, persisted states, and behavior after automatic or manual disappearance from the pending snapshot.
- [ ] 1.8 Add a focused health endpoint contract test for stable fields and failure behavior; decide from the result whether health can appear in the shared summary.
- [ ] 1.9 Run the targeted eSIM backend suites and record any failed contract as a blocking defect rather than proceeding with its UI entry.

## 2. Fix Verification Blockers Without Expanding Scope

- [ ] 2.1 Fix any Profile, download, confirmation, notification, history, note, health, or operation defect found by section 1 while preserving existing public request shapes.
- [ ] 2.2 Re-run all section 1 tests and confirm every action planned for the workbench has a passing current contract.
- [ ] 2.3 Document any capability that remains unavailable and keep its workbench control disabled or omitted with a capability explanation.

## 3. Refactor Frontend eSIM State

- [ ] 3.1 Add typed frontend health data only if task 1.8 confirms a stable response; otherwise remove unused health presentation assumptions.
- [ ] 3.2 Extend `useEsimStore` with bounded state for active Profiles/Notifications workspace, Pending/History mode, search/filter values, focused ICCID, per-notification action state, and expanded operation presentation.
- [ ] 3.3 Add computed Profile filtering by nickname, provider, ICCID, local tags, and enabled/disabled state without changing server snapshots.
- [ ] 3.4 Add computed pending/history filtering by text, event, Profile, and history state with clearable cross-navigation context.
- [ ] 3.5 Add coordinated refresh and terminal-operation convergence actions using only existing eSIM and operation API methods.
- [ ] 3.6 Keep existing action methods and typed ViewContext wiring operational while components are moved incrementally.

## 4. Build the Workbench Shell and Profile Experience

- [ ] 4.1 Replace the stacked composition with a shared eSIM summary and page-local Profiles/Notifications tabs on the existing route.
- [ ] 4.2 Implement the summary with verified EID/Profile/health/pending data, refresh and download commands, and distinct unavailable, unreadable, empty, loading, and partial-data states.
- [ ] 4.3 Implement a keyboard-accessible Profile search and enabled/disabled segmented filter.
- [ ] 4.4 Implement responsive Profile cards with textual state, masked ICCID, provider, class, local metadata, and one valid primary Enable/Disable action.
- [ ] 4.5 Move Rename, local metadata, related notifications, and Delete into clear contextual actions without hidden double-click or right-click dependencies.
- [ ] 4.6 Refactor the Profile editor to separate eUICC nickname from local label/phone/tags, preserve input on failure, and avoid rename calls when only local metadata changed.
- [ ] 4.7 Add target-specific Delete confirmation, keep Delete unavailable for enabled Profiles, and verify all Profile actions against section 1 contracts.

## 5. Build the Notification Experience

- [ ] 5.1 Implement Pending and History segmented views with independent counts and distinct loading, empty, no-match, and error states.
- [ ] 5.2 Add localized human-readable notification event and history state labels with text, event, Profile, and state filters as applicable.
- [ ] 5.3 Implement existing single-item Process and Remove actions with per-entry busy/error feedback and snapshot refresh after completion.
- [ ] 5.4 Add irreversible queue-removal confirmation that identifies the notification and explains the effect.
- [ ] 5.5 Add Profile-to-notification and notification-to-Profile navigation with visible, one-command-clearable ICCID filter context.
- [ ] 5.6 Verify pending/history transitions for process failure, process success, remove success, and stale notification snapshots against section 1 contracts.

## 6. Integrate the Existing Download Workflow

- [ ] 6.1 Extract one reusable QR decode/input helper from the current file-selection and clipboard-image implementation.
- [ ] 6.2 Add drag-and-drop image/text handling through the same helper while retaining manual activation-code and optional confirmation/matching inputs.
- [ ] 6.3 Build one download dialog or drawer that transitions from input to the accepted operation's progress without clearing the operation ID.
- [ ] 6.4 Move dynamic confirmation-code input into the same presentation and preserve submit, decline, cancellation, and timeout semantics of the existing endpoint.
- [ ] 6.5 Render existing staged download progress and structured terminal errors with retry that returns to the input state without duplicating an active operation.
- [ ] 6.6 Verify valid QR file, valid clipboard image, valid drag/drop, invalid image, manual code, initial confirmation code, dynamic confirmation, decline, failure, success, and reconnect scenarios.

## 7. Operation Feedback and Error Isolation

- [ ] 7.1 Implement an eSIM operation dock bound to the current operation ID for Enable, Disable, Delete, and Download accepted/running/progress/terminal states.
- [ ] 7.2 Disable only conflicting card-write actions while an operation is active and retain unrelated navigation and read-only inspection.
- [ ] 7.3 Refresh supported Profile, health, pending notification, and history snapshots after terminal success without treating request acceptance as success.
- [ ] 7.4 Preserve target and retry context after failed/cancelled operations and resynchronize through the existing operation endpoint after WebSocket interruption.
- [ ] 7.5 Isolate optional notes, health, and notification load failures so one failed auxiliary request does not falsely erase valid Profile data or appear as an empty list.

## 8. Accessibility, Responsive Design, and Localization

- [ ] 8.1 Add complete Chinese and English copy for tabs, filters, events, states, confirmations, operation feedback, empty/error states, and QR interactions without rendering raw backend keys.
- [ ] 8.2 Add scoped eSIM styles with a stable summary grid, responsive Profile grid, stacked narrow-screen notification actions, viewport-bounded dialogs, and no nested decorative cards.
- [ ] 8.3 Verify icon-only controls have tooltips and accessible names, keyboard focus follows the visible workflow, and state is not communicated by color alone.
- [ ] 8.4 Verify masked identifiers do not leak through visible labels, tooltips, filters, URLs, confirmations, or operation text.

## 9. End-to-End Verification

- [ ] 9.1 Run targeted eSIM Go tests and then `go test ./...`.
- [ ] 9.2 Run `npm --prefix web run typecheck`, `npm --prefix web run lint`, and `npm --prefix web run build`.
- [ ] 9.3 Start the demo backend and Vite frontend and verify every exposed Profile, notification, download, confirmation, and operation interaction against its existing API call.
- [ ] 9.4 Capture and inspect desktop, tablet, and mobile screenshots for both workspaces and all important loading, empty, failure, operation, and dialog states.
- [ ] 9.5 Confirm no eUICC detail editing, default SM-DP+, behavior-policy, batch-notification, SM-DS, reset, camera, or unrelated-page feature entered the implementation.
- [ ] 9.6 Update eSIM documentation to describe the interaction redesign while preserving the capability claims established by `complete-esim-management`.
