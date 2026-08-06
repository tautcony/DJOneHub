## Context

The full-repository code review (docs/code-review-report.md, baseline main @ 965b624) batch 2 covers reliability and lifecycle. The current code has: `EventBus.Publish` dropping events silently for slow subscribers (`internal/runtime/events.go:31-37`); AT/QMI backend event channels that block the command loop when full (`internal/backend/at_backend.go:120-152`, `qmi_backend.go:185-212`); a notification consumer that calls the Swift `Sink` synchronously inside the event loop (`internal/application/notification/service.go:98,156-236`) so a slow bridge drops `call.ended`/`sms.received` and the incoming-call card never closes; a Swift-to-Go command queue that silently drops `reject_call` (`internal/platform/darwin/native/bridge.go:295-299`) with no Swift-side timeout (`NativeUIHost.swift:306-326`); an `App.Stop` that stops only Notification+Runtime and closes the store while SMS/Network/Extras pollers still write (`internal/app/app.go:286-290`); an operation manager that detaches workers with `context.WithoutCancel` and has no cancel-all (`internal/application/operation/manager.go:46-48`); a hand-written events WebSocket with no read loop, no deadlines, snapshot-before-subscribe, and snapshot ID reuse (`internal/api/http/server.go:1037-1107`); a dual shutdown path in the HasUI branch of `cmd/djonehub/main.go` with `log.Fatal` inside the listen goroutine; a vowifihost `fail()` that never cleans up cancel/port plus per-event unbounded `Recover` goroutines (`internal/vowifihost/host.go:202-208`, `internal/application/vowifi/service.go:135-149`); and a zap logger that is never initialized (`pkg/logger/logger.go:17`, `cmd/djonehub/main.go`).

The sibling change `fix-security-and-data-loss` concurrently adds exact loopback binding and Origin/Host validation to the same WebSocket endpoint (`internal/api/http/server.go`) and introduces the `macos-native-ui` capability. This change must not duplicate or regress those requirements; where the areas overlap (WebSocket endpoint, native UI), the design builds on the sibling's decisions.

## Goals / Non-Goals

**Goals:**

- Event and notification delivery never blocks producers or the AT/QMI command loops, and every dropped event is counted and observable.
- Notification delivery to the native UI is decoupled from event consumption and converges on the true device state after recovery; user-critical bridge commands are never silently lost; reject-call state self-recovers.
- The application shuts down through exactly one path: workers stop in reverse start order and join, in-flight operations are cancelled, storage closes last, and nothing is delivered to a stopped UI.
- VoWiFi enable/disable/reconnect/recovery converge on one serialized state owner with complete failure cleanup and single-flight, debounced recovery.
- Device-layer zap logging is initialized at startup.
- The events WebSocket uses gorilla/websocket with connection hygiene and lossless snapshot ordering.

**Non-Goals:**

- The operation terminal-state retention cap and `DELETE /api/v1/operations/{id}` route (3.6 M1) are out of scope; only cancel-all-at-shutdown (`Shutdown(ctx)`) is added here.
- eSIM write-operation timeout semantics (3.1 M3) are out of scope (busy-state feedback, not shutdown/cancellation); only read-path context cancellation (3.1 M7) is included because it fits the cancel theme of this batch.
- The notification panel slot model and SMS notification redaction (3.9 M5/M1) remain out of scope per the sibling change's non-goals.
- No metrics infrastructure is introduced; drop counters are exposed as an additive `event_drops` object on the existing notification-debug diagnostics response.
- No new API endpoints or storage schema changes; the existing notification-debug response gains diagnostic counters, and the events WebSocket wire protocol is preserved.

## Decisions

### D1. Non-blocking event publishing everywhere, with drop accounting

`EventBus.Publish` already uses non-blocking sends with a silent `default`; it gains per-subscriber drop counters (atomic counters keyed by subscription) and a cumulative counter. The existing `GET /api/v1/notifications/debug` response gains an additive `event_drops` object with the cumulative count and active-subscriber counters; unsubscribe removes the subscriber entry so diagnostics cannot retain stale subscriptions. AT and QMI backend `Events` implementations are converted from blocking sends (`at_backend.go:120-152`, `qmi_backend.go:185-212`) to the same select-default pattern and expose their cumulative counters through the same object, so a slow consumer can no longer stall the AT command loop or QMI event dispatch (3.3 H7).

*Alternatives considered*: blocking sends with an internal queue per backend — rejected because it only moves the backpressure into the backend and still allows one slow consumer to stall device command processing; unbounded buffered channels — rejected because memory growth under a permanently stalled consumer is worse than a counted drop. A global drop counter instead of per-subscriber was rejected because the report's intent is diagnosability of which consumer is losing events.

### D2. Notification consumer decoupled from the Swift sink, with reconciliation

The notification service keeps consuming `EventBus` events in its existing goroutine (`notification/service.go:98-118`), but `handle()` no longer calls `s.config.Sink.*` inline. Sink calls are enqueued into an internal bounded queue consumed by a dedicated delivery goroutine that is the only caller of the `Sink` interface, mirroring the bridge's own command loop. The sink queue is bounded like the event subscription; if the bridge is slow, notification events are dropped from the *sink* queue (counted) but never from the event consumer.

After the sink recovers (the delivery queue records a drop or the bridge reports an unavailable UI), the service re-derives state from the application services: the active call from the extras service's polled call state, missed calls and recent SMS from the SMS service, and re-issues `ShowCall`/`ShowSMS`/`HideCall`/`ShowMissedCall` so the UI converges even for events that were dropped (2.4). The delivery goroutine stops accepting new work during shutdown, drains only the bounded queue within the shutdown deadline, and never invokes the sink after the native UI has stopped. This reuses the baseline/reconcile machinery the service already has in `Start` (`seenCalls`/`seenSMS`) and its existing test harness with the `recordingSink`.

*Alternatives considered*: increasing the event subscription buffer — rejected because it only delays the drop and still loses `call.ended` when the bridge is slow; replaying the whole event log — rejected because the bus has no retention. Re-deriving from the extras/SMS services is exactly the reconciliation the report suggests and matches the data the notification policy already trusts.

### D3. Native UI command delivery: feedback on drop, and a Swift reject timeout

The queue at `bridge.go:295-299` carries Swift user commands into Go. Its non-blocking select remains, but a `default` branch no longer disappears silently: Go records the rejection and sends a Go-to-Swift `command.dropped` event with command name and reason through `uiDriver.handleEvent`. Swift uses that feedback to clear the pending state for reject-call or leave the original action available for retry; Go never emits a started/succeeded result for a command it did not enqueue. This is the "command.dropped / ui.error 回传" from 2.4 and 3.9 M2/M4.

Swift-side `NativeUIHost` gets a bounded timeout (5-10 s) on the `rejectingCallID` state: if no `callRejectSucceeded`/`callRejectFailed` arrives in time, the UI clears the rejecting state and restores the buttons (3.9 M3), so a lost command or unresponsive device can never strand the panel. The `callRejectFailed` handler also clears state for any call ID (the report notes the current unmatched-ID bug).

*Alternatives considered*: blocking the Go send with a timeout — rejected because it reintroduces backpressure onto the caller thread and still needs a timeout on the Swift side; relying only on the Swift timeout without a Go-side feedback channel — rejected because lost user commands would then be invisible in Go logs and diagnostics.

### D4. Shutdown ordering: stoppable pollers, cancel-all operations, join before close

- Each polling service (SMS, network, extras) gains the notification service's `Stop` pattern: a cancel function plus a `done` channel created in `Start`, `Stop(ctx)` that cancels and waits with the shutdown deadline, and idempotent repeated calls (`notification/service.go:120-131` is the model). Extras' internally started poller gets the same treatment.
- `App.Start` currently starts Runtime, SMS, Network, Extras, and Notification in that order. Shutdown first closes an idempotent admission gate shared by HTTP handlers and `operation.Manager`, then stops accepting HTTP connections. `App.Stop(ctx)` calls `operation.Manager.Shutdown(ctx)` to cancel and join detached operations, then stops workers in reverse start order (Notification, Extras, Network, SMS, Runtime) and waits for each to join before closing the store (`app.go:286-290`). A timed-out worker stop returns the documented shutdown error and leaves the store open while any worker may still write; a separate final recovery path may force process exit, but storage is never closed underneath a live writer (2.5, 3.6 M4).
- `operation.Manager` gains `Shutdown(ctx)`: it marks the manager closed so no new operation can start, cancels every tracked operation via the existing `cancels` map, and waits for their `run` goroutines (a WaitGroup) within the bounded context. `Start` changes from `string` to `(string, error)` and returns the structured shutdown/unavailable error after closure; no empty-ID sentinel or compatibility fallback remains. Repeated shutdown calls share one close signal while each caller waits with its own context, so an early caller timeout does not permanently become the result for later callers; workers already classify `ctx.Err()` as `Cancelled` (3.10 M2).
- eSIM read paths (`manager.go:1730-1732` overview/EID/profiles, `at_port.go:40-56`) thread the request context through and check `ctx.Err()` between per-AID APDU steps so a cancelled request stops promptly and releases `opMu`/the arbiter (3.1 M7). `at_port.go` also receives the device-level arbiter wiring in `cleanup-architectural-debt` D10 (a constructor change); the two edits are disjoint — context threading here, constructor wiring there — and land independently.

*Alternatives considered*: a generic supervisor that owns all goroutines — rejected as a larger refactor than the review calls for; the report explicitly points at the notification `Stop` pattern, so we copy that proven shape rather than invent a new one.

### D5. Single shutdown path

`cmd/djonehub/main.go` converges on one `shutdown()`: close application/operation admission first, stop accepting new HTTP connections, drain active handlers within a dedicated HTTP deadline, then run `instance.Stop()` with a separate worker deadline. Closing admission before HTTP drain prevents an already-connected handler from starting new detached work after shutdown begins.

- In the HasUI branch, the separate `<-ctx.Done()` goroutine that ran `NativeUI.Stop()` + `shutdown()` concurrently with the main-thread path is removed; a single goroutine selects on both UI-exit and `ctx.Done()` and runs the one shutdown sequence, so `NativeUI.Stop()` and `shutdown()` can never execute twice or in parallel (3.10 M3).
- `Bridge.send` is gated on the existing `started && !exited` state (the callback path already gates this way; the send path currently does not) so no event is posted to a stopped AppKit run loop (2.5).
- `ListenAndServe` failure no longer calls `log.Fatal` from inside the goroutine; the listener is bound (`net.Listen`) before the UI starts so a bind failure is detected before `NativeUI.Start`, and serve errors are propagated to the main flow which runs the normal shutdown path (3.10 L7).

*Alternatives considered*: leaving the UI path and signal path independent with a mutex-protected `shutdown()` — rejected because the report identifies the concurrent double-stop itself as the bug and a flag-only guard still leaves two code paths with divergent teardown sequences.

### D6. Events WebSocket migration to gorilla/websocket with lossless snapshot ordering

The hand-written upgrade (`server.go:1037-1082`) is replaced by `github.com/gorilla/websocket` (already present in `go.mod`; this migration introduces its first use), which enforces GET and `Sec-WebSocket-Version: 13` natively (3.10 L2). The migration preserves the loopback Origin/Host checks added by `fix-security-and-data-loss` (`Upgrader.CheckOrigin` for the origin policy). Migration steps:

1. Swap the hijack+handshake for `websocket.Upgrader{CheckOrigin: ...}.Upgrade(w, r, nil)`, keeping the existing `protected()` gate and the exact `http://127.0.0.1:<port>` Origin/Host policy from `fix-security-and-data-loss` before it. This is an origin/host check only; no login or credential check is added.
2. Add a read loop: `SetReadDeadline` + `ReadMessage` (discarding payloads) with a pong handler that extends the read deadline, and `SetWriteDeadline` on the write path; ping on a keepalive interval (3.6 H2). All event and ping writes go through one writer goroutine or write mutex because gorilla permits only one concurrent writer. A session whose reads fail the deadline is closed and unsubscribed.
3. Fix ordering with an explicit watermark: add `EventBus.SubscribeWithWatermark(buffer)` returning a subscription handle, its event channel, the captured sequence, a drop counter, and an idempotent unsubscribe function. The bus subscribes and captures the sequence under one lock. Build the device snapshot and send it with that watermark as its ID; after the snapshot, forward every queued event with `ID > watermark`, including events published while the snapshot was being built. The snapshot covers device status only; operation, SMS, and call events are not discarded as snapshot-covered. If this WebSocket subscription overflows, its drop counter is checked and the connection is closed so the client reconnects and obtains a fresh snapshot; the event bus diagnostic counter remains the source of drop accounting (2.5, 3.6 H2).
4. The `writeTextFrame` >65535-byte drop is eliminated by gorilla's automatic frame fragmentation; the current `Event` JSON envelope, the `snapshot` event type, and `publicEvent`/`publicDeviceStatus` sanitization are unchanged.

*Alternatives considered*: keeping the hijack path and adding deadlines by hand — rejected because the report calls for gorilla/websocket explicitly and hand-rolled keepalive on a hijacked connection is exactly the fragile code being replaced; moving snapshot-then-subscribe and using `LastID()+1` as a client-side watermark — rejected in favor of subscribe-first, which removes the loss window by construction rather than by arithmetic.

### D7. vowifihost state machine convergence and single-flight recovery

- `Host.fail` (`host.go:202-208`) becomes the single cleanup path: it cancels the stored child context, closes any opened port, and clears `h.cancel`/`h.port` before setting `Failed`, so repeated failed enables cannot leak modem ports or event consumers (3.6 H1).
- All transitions (`Enable`, `Disable`, `Reconnect`, `Recover`, `DeviceRemoved`, event-driven recovery) are serialized through one transition lock (or a single goroutine actor) so no two transitions interleave on the same port; the application-level `followRuntime` and host-level `consumeEvents` event-driven `go Recover(...)` calls (3.4 H2, 3.6 H1) are replaced by a single-flight trigger with a debounce window, and recovery runs under the `ResourceVoWiFi` lock so it cannot race user Enable/Disable.
- The runtime-event subscription is tied to the session lifecycle: `followRuntime` holds the unsubscribe and exits when the session context ends (currently the subscription is created and the goroutine never stops — `app/vowifi/service.go:127-150`), and `consumeEvents` already exits on `ctx.Done()`.

*Alternatives considered*: keeping event-driven `go Recover` but adding a mutex inside `Recover` — rejected because concurrent goroutines would still interleave with user operations at the call sites; a full external FSM library — rejected as over-engineering for five states; a lock plus debounce preserves the existing `State` enum and tests.

### D8. Logger wiring

`cmd/djonehub/main.go` calls `logger.Setup` (with a `LogConfig` derived from the existing CLI flags) before `app.New`/`instance.Start`, so the zap logger replaces `zap.NewNop()` and device-layer logs from modem, backend, and eSIM become visible (2.6, 3.10 M1). Log level configuration is wired through the existing flag if present; otherwise the default debug-derived level from `getLogLevel` is used.

*Alternatives considered*: unifying on stdlib `log` instead — rejected because the device layers already use zap throughout and the report's suggested fix is to call `Setup`; removing `pkg/logger` entirely belongs to the third batch's dead-code cleanup, not here.

## Risks / Trade-offs

- [Dropping events is still lossy even when counted] → The notification path no longer relies on bus delivery for correctness: reconciliation (D2) re-derives UI state from the extras/SMS services, so counted drops degrade to a brief delay, not permanent loss.
- [Notification sink queue bound is a new drop point] → The bound is sized relative to the bridge's command queue and the drop is counted and reconciled; the alternative (unbounded) risks memory growth during a long bridge stall.
- [gorilla/websocket migration may change handshake error responses] → The loopback Origin checks run before/inside the upgrade and are unchanged; gorilla returns its own upgrade errors, which the API layer maps to the existing structured error format.
- [Subscribe-before-snapshot ordering relies on ID monotonicity] → the subscribe-and-watermark operation captures the sequence under the bus lock; all events after that watermark are forwarded after the snapshot instead of being treated as covered by device status.
- [Serializing VoWiFi transitions can delay a user Enable behind a slow Reconnect] → Recovery is debounced and single-flight, and the transition lock is held only across the port operation; user Enable still runs first where the lock is contended and the UI already shows transitions via `vowifi.updated` events.
- [Shutdown waits for pollers that may be stuck on a device read] → Poller stop cancels the poll context, and device read paths honor cancellation (D4); the wait is bounded by the operation `Shutdown(ctx)` timeout.
- [Cancelling operations at shutdown changes the terminal state clients see] → Workers already report `Cancelled` when `ctx.Err()` is set; the UI already renders the cancelled terminal state, so this is consistent behavior, not a new error surface.

## Migration Plan

- Land in review-batch order, each unit independently reviewable and revertible: (1) event-bus and backend drop accounting; (2) notification sink decoupling + reconciliation, bridge drop feedback, Swift reject timeout (macOS side lands with its Go side); (3) shutdown ordering (poller stops, `App.Stop`, operation `Shutdown(ctx)`, eSIM read cancellation); (4) single shutdown path in main; (5) WebSocket gorilla migration (preserving the sibling change's loopback/origin checks — coordinate to land after `fix-security-and-data-loss` to avoid rebasing the hand-written upgrade twice); (6) vowifihost state machine; (7) `logger.Setup` wiring.
- Rollback: revert a unit's commit; the gorilla migration and additive diagnostics field can be reverted independently (no `go.mod` change is introduced by this change). No storage migration is introduced, and the events WebSocket wire protocol remains unchanged.
- Coordination: the WS loopback/origin checks and the `macos-native-ui` capability are shared with `fix-security-and-data-loss`. This change holds the single authoritative snapshot requirement: the security sibling's duplicate MODIFIED block was removed, and this block carries forward the loopback/no-login boundary while adding watermark ordering; the remaining deltas are additive to the sibling's requirements.

## Open Questions

- Whether the sink queue bound and reject-call timeout should be configuration values or constants (report suggests 5-10 s for the timeout; a constant with a test is the default).
- The event-drop counters are exposed on the existing `GET /api/v1/notifications/debug` response under `event_drops`; no endpoint choice remains open.
- Whether the debounce window for VoWiFi recovery should match the modem reset event cadence or be fixed (e.g., 2-5 s) — validate against real device event rates during implementation.
- Confirm the `logger.LogConfig` fields the CLI can populate (filename/rotation/level flags) before wiring `Setup`, so the third batch's config cleanup does not collide.
