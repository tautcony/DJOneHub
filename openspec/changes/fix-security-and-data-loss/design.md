## Context

The full-repository code review (docs/code-review-report.md, baseline main @ 965b624) identified six first-priority defects spanning security and data loss. Login authentication is deferred until its first-use/bootstrap flow is designed. The temporary control-plane boundary is therefore a loopback-only bind requirement (`127.0.0.1`, `localhost`, or `::1`) plus request-origin protections. The codebase already contains most of the infrastructure needed for the remaining fixes: a new-SMS hook with no root-module consumer (`internal/modem/manager.go`), a `Reassembler` in `pkg/smscodec` with no cross-poll state (`internal/application/sms/service.go:338-356`), and Swift 6 `@MainActor`-isolated notification delegate methods (`macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift:121,377-401`).

## Goals / Non-Goals

**Goals:**

- Close the temporary local control-plane hole: reject any listen address whose host is not a loopback form (`127.0.0.1`, `localhost`, `::1`), validate Origin and Host on WebSocket upgrades and state-changing requests, and reject cross-site simple requests. Login authentication is out of scope.
- Make the SMS pipeline end-to-end usable: true storage indices, decoded PDU content, consumer-owned inbound delivery, persistent cross-poll reassembly, serialized storage switching.
- Make AT command responses deterministic: no residual-response leakage after timeout, precise prompt matching, URC-before-prompt dispatch, configurable watchdog threshold.
- Eliminate the eSIM synchronization races: race-free write-completion notification and force-release semantics that preserve exclusivity.
- Bound all parsing of device-controlled data (MBIM counts, fragment collectors, modem line buffer).
- Make macOS notification delegate callbacks crash-proof under Swift 6 actor isolation.

**Non-Goals:**

- Event/notification reliability, shutdown ordering, VoWiFi lifecycle, logger wiring, and dead-code removal are out of scope (they belong to later review batches).
- No login authentication, credential/token generation, user account system, remote listening, or multi-device support.
- No changes to the notification panel slot model or SMS privacy/redaction (later batch).
- No protocol changes beyond what the listed fixes require.

## Decisions

### D1. Temporary boundary: loopback-only listen

- In `cmd/djonehub/main.go`, parse the listen address and fail before application/UI startup unless the host is a loopback form: `127.0.0.1`, `localhost`, or `::1` (`[::1]` when bracketed). Any wildcard (`0.0.0.0`, `::`), non-loopback, or hostname address fails startup.
- Do not wire `Authenticator`, generate or persist tokens, activate `web.username/password`, add login endpoints, or add frontend credential handling in this change. The existing authentication interface and error type remain reserved for a later change.
- *Alternatives considered*: allowing non-loopback with credentials — deferred because the first-use/bootstrap flow is not yet defined; accepting only one loopback form — rejected because browsers and tools legitimately address the service as `127.0.0.1`, `localhost`, or `::1`, and all three are equally local.

### D2. Cross-site protections on WebSocket and write endpoints

- The hand-written event WebSocket upgrade (`server.go:1037-1082`) and the ADB shell WebSocket (`server.go:628`, currently `CheckOrigin: func(...) bool { return true }`) both validate Origin and Host: the host part must be a loopback form (`127.0.0.1`, `localhost`, `::1`) and the port must be the bound port, so `http://127.0.0.1:<port>`, `http://localhost:<port>`, and `http://[::1]:<port>` all pass while any other origin is rejected.
- Every state-changing endpoint applies the same request guard before invoking a service. Body-bearing writes require `Content-Type: application/json`; empty-body POSTs also require an allowed loopback Origin and same-site `Sec-Fetch-Site` when supplied. Missing or disallowed origin metadata is rejected for state-changing requests.
- The frontend remains a same-origin loopback client and sends no credentials.
- *Alternatives considered*: CSRF tokens in a cookie — deferred with login authentication; strict loopback binding plus Origin/Host and state-changing request checks provides the temporary boundary without session state.

### D3. SMS pipeline repair

- `internal/modem/manager.go`: the `+CMTI` path no longer unconditionally `AT+CMGR`+`AT+CMGD`; it emits a `NewSMSRef{Storage, Index}` only through a registered consumer. `SetNewSMSHandler` changes to carry both storage and index. With no consumer, the URC is logged and the message remains in modem storage. The whole switch-storage / read / restore sequence (`manager.go:1381-1430,1502-1532`) is serialized by a per-Manager `smsReadMu` flow-level mutex so concurrent `+CMTI` bursts cannot interleave.
- `internal/backend`: define the `NewSMSRef` and consumer-registration/acknowledgement contracts. `ATBackend` forwards the modem registration, and `BusinessAdapter` forwards it to the runtime-owned SMS service during backend installation; QMI/MBIM backends implement the same decoded message contract even when their notification source is not `+CMTI`. The consumer reads by the reference's storage and index and obtains a non-empty stable SIM identity before acknowledging. Incomplete multipart segments remain unacknowledged in modem storage and are tracked idempotently in memory by `(SIM identity, storage, index)`; once the complete message is durably persisted, every component reference is acknowledged/deleted. Durable persistence includes a conflict-ignored insert: `INSERT OR IGNORE` reporting zero rows affected means the identical message already exists in storage, which counts as persisted and permits acknowledgement. EventBus publication is best-effort and is not a durable acknowledgement. A failed read, decode, identity lookup, reassembly, or persistence leaves all affected modem entries intact; the next refresh retries them. Duplicate reads do not re-emit a completed message. Shutdown unregisters the consumer before backend close.
- On startup, the first SMS refresh establishes a baseline only: messages re-read from retained modem entries (e.g., a crash between persistence and acknowledgement) are deduplicated against stored state and are not re-published or re-notified as fresh (report 3.4 L2); subsequent refreshes publish new deliveries normally.
- *Ownership*: this change owns the SMS delivery pipeline (modem, backend, application, `pkg/smscodec`). Storage-layer persistence changes (deduplication key including SIM identity, paginated `ListSMS` signatures — report 3.6 L2) are owned by `cleanup-architectural-debt` D16; this change calls the storage API with current signatures, and D16 updates this service's caller in coordination.
- `internal/modem`: `SMSListAllPDU` returns real `(index, PDU)` pairs instead of synthetic loop indices, preserving the storage identity needed by list/read/delete.
- `internal/backend`: `at_backend.go` and `qmi_backend.go` `ReadSMS` decode the PDU (via `pkg/smscodec`) inside the backend, and `SMSSummary`/`ListSMS` carry `ReceivedAt` and the storage identity. `BusinessAdapter.ListSMS` copies every summary field instead of returning index-only placeholders; all production and fake implementations are updated together.
- `internal/application/sms/service.go`: the `Reassembler` becomes a persistent field of the SMS service, mutex-protected, with TTL cleanup replacing the per-`Refresh` construction (`service.go:338-356`). The service owns consumer registration, delivery acknowledgement, retry, and unregister-on-stop through the backend/runtime path. Incomplete segments remain in modem storage until the complete message is durably recorded; process-restart recovery is best-effort because the in-memory reassembly cache is rebuilt by rereading those retained entries.
- `pkg/smscodec/reassembler.go`: the reassembly cache key includes the total segment count, and `Add` validates consistency, preventing 8-bit reference wraparound from polluting unrelated messages.
- *Alternatives considered*: keeping auto-read-delete in the manager and buffering inside the manager — rejected because the manager has no storage or notification context and the application layer already owns the SMS use case.

### D4. AT response correctness

- After a command timeout (`manager.go:550-564`), the manager quarantines the command stream and does not accept another command until the timed-out command's terminal response is observed. If no terminal response arrives before a bounded transport-recovery deadline, the manager closes/reinitializes the AT transport; a fixed short drain is only an optimization inside quarantine and is never the correctness guarantee. This prevents a late OK from short-circuiting the next command and stale data from being spliced into its `fullResponse`.
- Prompt detection (`manager.go:596-616`) matches only a bare `"> "`/`">"` line and only when the command is an interactive command waiting for a prompt; URC detection runs before the prompt branch so a `>` inside a URC/USSD line is dispatched as a URC and never terminates the current command. `readLoop`'s `strings.HasSuffix(data, ">")` half-line heuristic is removed.
- The consecutive-timeout watchdog threshold (`manager.go:337-360`) becomes configurable, and long-running commands are excluded from the timeout counter.
- *Alternatives considered*: a short bounded drain as the sole isolation mechanism — rejected because a response can arrive after the drain window; per-command generation IDs remain an alternative, while quarantine plus transport recovery provides the required boundary without attributing untagged bytes to a new command.

### D5. eSIM synchronization primitives

- `internal/esim/manager.go` `opDone` (lines 897-958): the "close old channel, then replace with new" sequence moves under the protecting mutex (`cacheMu`), or is replaced by `atomic.Pointer[chan struct{}]`, so a reader can never evaluate a never-closed channel and wait a full 5-second timer for a false `ErrOperationInProgress`.
- `internal/apduarbiter/arbiter.go` (lines 441-443, 702-730): MaxLeaseHold force-release notifies the holder (flag/notification on the lease) instead of silently taking it away; `lease.Touch()` results are honored so progressing leases are not force-released; and the arbiter treats a force-released lease's in-flight APDU as still occupying the device — no new exclusive lease or SIM-switch barrier is granted until that APDU truly finishes. If the APDU exceeds the transport recovery deadline, the arbiter marks the transport quarantined and reports it to the transport owner (the modem manager), which performs the existing close/reinitialize sequence; the arbiter never closes a transport it does not own, and must not leave the device permanently wedged behind an unbounded in-flight operation. While quarantined, no new APDU work is admitted until the owner confirms recovery.
- *Ownership*: this change owns the arbiter's lease/force-release/watchdog semantics (`arbiter.go:441-443,702-730`). The legacy `AcquireSession`/`AcquireOneShot` deletion (`arbiter.go:276-280`) and the MBIM/QMI scope documentation (`arbiter.go:902-910`) are owned by `cleanup-architectural-debt` D2/D12 — disjoint line ranges; each change leaves the other's regions untouched.
- *Alternatives considered*: cancelling the in-flight APDU's context at force-release — rejected because terminating an APDU mid-transmission can leave the card or modem in an undefined state; waiting for completion preserves exclusive semantics with bounded delay; the arbiter directly closing the transport — rejected because transport lifecycle belongs to the modem manager, and a double-close would race with the manager's own reconnect path.

### D6. Bounded protocol parsing

- `pkg/mbim/sms.go`, `providers.go`, `databuffer.go`, `uicc_fileaccess.go`: before `make([]T, 0, count)`, validate `count <= (len(buf)-header)/elemSize`; fall back to append-driven parsing so the `u32At` boundary checks terminate hostile input.
- `pkg/mbim/fragment.go`: per-collector caps on accumulated bytes and fragment count; collectors are removed on context cancellation/timeout (currently only `removePending` runs), with a periodic sweep of stale incomplete collectors.
- `internal/modem/manager.go:643-745`: the `lineBuf` is capped (4 KB); a sustained over-limit stream is treated as device misbehavior rather than unbounded growth, and over-long lines never enter `fullResponse`.
- *Alternatives considered*: streaming parse without intermediate buffers — rejected as a rewrite of the mbim layer; caps plus validation preserve the existing structure.

### D7. macOS notification delegate isolation

- `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift` (lines 377-401): the `UNUserNotificationCenterDelegate` methods (`willPresent`, `didReceive`) become `nonisolated`; each method wraps state access in `Task { @MainActor in ... }` and invokes the `completionHandler` synchronously inside the callback, satisfying the system requirement that the handler be called within the callback while keeping actor-safe state access. The Go bridge thread contract (LockOSThread + main-thread AppKit) is unchanged.
- *Alternatives considered*: hopping to the main queue and calling the completion handler asynchronously — rejected because `UNUserNotificationCenter` requires the completion handler to be called inside the callback; leaving the methods `@MainActor` — rejected because dynamic isolation checks abort the process on background-queue callbacks during cold-start launch.

## Risks / Trade-offs

- [Strict bind validation breaks remote deployments] → This is intentional while login authentication is deferred; a later authentication change can broaden the bind policy after its bootstrap flow is specified.
- [Transport quarantine may delay subsequent commands] → the manager waits only until the terminal response or the bounded transport-recovery deadline; a stuck stream is reinitialized instead of allowing an unbounded wait or cross-command response leak.
- [`smsReadMu` serialization reduces SMS throughput under bursts] → Correctness (no missed or interleaved messages) is prioritized; the critical section is limited to the CPMS switch/read/restore sequence.
- [Excluding long commands from the watchdog could mask a hung device] → The threshold remains configurable; progress (`Touch`) still resets the counter, so only genuinely stuck commands accumulate.
- [Force-release no longer grants exclusivity quickly] → The wait is bounded by the in-flight APDU's remaining time or the transport recovery deadline; `Touch()` progress prevents healthy long APDUs from being force-released at all, while a stuck APDU triggers quarantine/reinitialization rather than an unbounded block.
- [Swift `nonisolated` delegate refactor risks subtle main-actor state races] → All state access is confined to `Task { @MainActor in }`; tests must cover cold-start (notification-click) launch.

## Migration Plan

- Land in the review's priority order: (1) exact loopback binding plus cross-site protections; (2) SMS pipeline; (3) AT response correctness; (4) eSIM synchronization primitives; (5) parsing bounds; (6) macOS delegate isolation. Each unit is independently reviewable and revertible.
- Rollback: revert a unit's commit; no credential or storage migration is involved.
- No storage schema changes are required by this change.

## Open Questions

- Whether the watchdog timeout threshold is exposed as a CLI flag, config file value, or both.
- Whether the QMI SMS path needs the same storage-switch serialization as the AT path, or whether QMI's delivery model makes it unnecessary — confirm against the QMI backend during implementation.
