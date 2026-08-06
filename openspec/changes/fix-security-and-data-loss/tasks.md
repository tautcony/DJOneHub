## 1. Loopback Boundary and Cross-Site Protections (P0)

- [ ] 1.1 Reject every `-listen` host that is not a loopback form (`127.0.0.1`, `localhost`, `::1`) before `app.New`/UI startup; add tests for wildcard, non-loopback, and hostname addresses and for all three accepted loopback forms
- [ ] 1.2 Validate Origin and Host on the event WebSocket upgrade (internal/api/http/server.go): host part must be `127.0.0.1`, `localhost`, or `::1` with the bound port
- [ ] 1.3 Replace the ADB shell WebSocket's `CheckOrigin: true` with the same loopback Origin/Host whitelist
- [ ] 1.4 Apply state-changing request protection to every write endpoint, including empty-body POSTs: require JSON for body-bearing writes and reject disallowed or missing Origin/Sec-Fetch-Site values before invoking services
- [ ] 1.5 Keep OpenAPI free of login security requirements and add integration tests for bad WS Origin/Host, cross-site `text/plain`, cross-site empty-body POST, and allowed same-origin requests over `127.0.0.1`, `localhost`, and `[::1]`

## 2. SMS Pipeline Repair (P1)

- [ ] 2.1 Define `backend.NewSMSRef{Storage, Index}` and a consumer registration contract; thread it from `ATBackend`/`BusinessAdapter` to the runtime-owned SMS service, and update production/fake contracts together
- [ ] 2.2 Change the `+CMTI` handler in `internal/modem/manager.go` to emit both storage and index only when a consumer is registered; with no consumer, retain the modem entry and never auto-delete it
- [ ] 2.3 Make the consumer read by the exact reference, require a non-empty stable SIM identity, retain incomplete multipart segments in modem storage, and acknowledge/delete every component reference only after durable complete-message persistence — including a conflict-ignored insert (rows affected = 0) as durable; retain all entries on identity/read/decode/reassembly/persistence failure for refresh retry, unregister before backend shutdown, and do not use EventBus delivery as the durable acknowledgement gate. Storage API signatures are owned by `cleanup-architectural-debt` D16; call current signatures here and coordinate caller updates there
- [ ] 2.4 Serialize the storage-switch/read/restore sequence with a per-Manager `smsReadMu` flow-level mutex (manager.go CPMS path)
- [ ] 2.5 Make `SMSListAllPDU` return real `(index, PDU)` pairs; update `SMSSummary`, `at_backend.go`, `qmi_backend.go`, `BusinessAdapter.ListSMS`, and all fakes to preserve `Index`, storage/tag, and `ReceivedAt`
- [ ] 2.6 Decode PDU content inside `ReadSMS` in the AT and QMI paths via `pkg/smscodec`, with malformed PDU errors returned without deletion
- [ ] 2.7 Promote the `Reassembler` to a persistent, mutex-protected field of the SMS service with TTL cleanup (internal/application/sms/service.go), and test multipart delivery across refresh cycles
- [ ] 2.8 Include the total segment count in the `pkg/smscodec/reassembler.go` cache key and validate segment consistency on `Add` to survive 8-bit reference wraparound
- [ ] 2.9 On startup, make the first SMS refresh a baseline only: deduplicate retained modem entries against stored state (dedup by content key or stored-record check) and do not re-publish or re-notify them as fresh; subsequent refreshes publish new deliveries (report 3.4 L2)

## 3. AT Response Correctness (P1)

- [ ] 3.1 After a command timeout, quarantine the command stream until its terminal response is observed; if it does not arrive before a bounded transport-recovery deadline, close/reinitialize the AT transport before accepting another command. URC dispatch continues during quarantine (a `+CMTI`/`+CUSD` in the window is delivered, not dropped or attributed to the quarantined command). A short best-effort drain alone is not sufficient (manager.go)
- [ ] 3.2 Restrict prompt detection to a bare `"> "`/`">"` line and only when an interactive command is waiting for a prompt (manager.go); remove the `HasSuffix(data, ">")` half-line heuristic in `readLoop`
- [ ] 3.3 Move URC detection before the prompt branch so lines containing `>` are dispatched as URCs and never terminate the running command
- [ ] 3.4 Make the consecutive-timeout watchdog threshold configurable and exclude long-running commands from the timeout counter (manager.go watchdog)

## 4. eSIM Synchronization Primitives (P1)

- [ ] 4.1 Make the `opDone` close-old/replace-new sequence race-free in `internal/esim/manager.go` (close and replace under `cacheMu`, or `atomic.Pointer[chan struct{}]`) so readers never wait a full 5-second timer for a false `ErrOperationInProgress`
- [ ] 4.2 Notify a lease holder when `MaxLeaseHold` force-releases its lease, and honor `lease.Touch()` progress so active leases are not force-released (internal/apduarbiter/arbiter.go)
- [ ] 4.3 Do not grant a new exclusive lease or SIM-switch barrier while a force-released lease's APDU is still in flight; treat the in-flight APDU as occupying the device until it finishes, or until the transport recovery deadline marks it quarantined and reports it to the transport owner (modem manager), which runs the close/reinitialize sequence — the arbiter never closes a transport it does not own; test completion and stuck-transport paths. The legacy `AcquireSession`/`AcquireOneShot` deletion and MBIM/QMI scope documentation live in `cleanup-architectural-debt` D2/D12 (disjoint line ranges in arbiter.go)

## 5. Protocol Parsing Limits (P1)

- [ ] 5.1 Validate device-controlled counts against the remaining buffer before preallocation in `pkg/mbim/sms.go`, `providers.go`, `databuffer.go`, and `uicc_fileaccess.go` (or switch those paths to append-driven parsing)
- [ ] 5.2 Cap per-collector accumulated bytes and fragment count in `pkg/mbim/fragment.go`, remove collectors on context cancellation/timeout, and add a periodic sweep of stale incomplete collectors
- [ ] 5.3 Cap the modem line buffer at 4 KB, treat sustained over-limit input as device misbehavior, and keep over-long lines out of `fullResponse` (internal/modem/manager.go)

## 6. macOS Notification Delegate Isolation (P1)

- [ ] 6.1 Make the `UNUserNotificationCenter` delegate methods `nonisolated` in `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift`, wrap state access in `Task { @MainActor in ... }`, and invoke the completion handler synchronously within the callback

## 7. Verification

- [ ] 7.1 Add integration tests for the temporary boundary: non-loopback bind, cross-site `text/plain` and empty-body writes, WS upgrade with bad Origin/Host, and allowed same-origin requests over `127.0.0.1`, `localhost`, and `[::1]`
- [ ] 7.2 Add SMS tests: inbound SMS with no consumer is retained, callback carries storage plus index, missing SIM identity and failed acknowledgement retain the entry for retry, a conflict-ignored insert (rows affected = 0) still acknowledges, list/read/delete use true indices and `ReceivedAt`, duplicate segment reads are idempotent, multipart SMS reassembles across refresh cycles, retained pre-crash messages are not re-published by the startup baseline refresh, and reassembly key survives ref wraparound; document process-restart delivery as best-effort
- [ ] 7.3 Add AT tests: late final response after timeout does not leak into the next command, `>`-containing URC dispatched correctly, watchdog threshold behavior
- [ ] 7.4 Add parsing tests: hostile MBIM counts, never-completing fragment streams, over-limit modem lines
- [ ] 7.5 Add an eSIM test that a read issued after write completion never returns a false operation-in-progress, and a force-release test that exclusivity is preserved while an APDU is in flight
- [ ] 7.6 Run `go test -race` over modem, esim, apduarbiter, backend, and application packages and fix any reported races
- [ ] 7.7 Verify the macOS notifier launches from a notification click without aborting, and validate OpenAPI output does not advertise deferred login authentication
