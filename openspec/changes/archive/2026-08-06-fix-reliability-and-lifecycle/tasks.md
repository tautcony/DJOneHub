## 1. Event and Notification Reliability (D1, D2)

- [x] 1.1 Add per-subscriber and cumulative drop counters to `EventBus.Publish` in `internal/runtime/events.go`; expose a read-only `event_drops` diagnostic object through the existing `GET /api/v1/notifications/debug` response (no new endpoint), including cumulative and active-subscriber counts
- [x] 1.2 Convert the AT backend `Events` channel send to non-blocking with drop counting (`internal/backend/at_backend.go`) so a slow consumer cannot stall the AT command loop
- [x] 1.3 Convert the QMI backend `Events` channel send to non-blocking with drop counting (`internal/backend/qmi_backend.go`) so a slow consumer cannot stall event dispatch
- [x] 1.4 Add an internal bounded sink queue and a dedicated delivery goroutine to the notification service (`internal/application/notification/service.go`) so `handle` never calls the `Sink` synchronously; define `Stop(ctx)` to stop accepting work, drain only until the deadline, abort queued work on timeout, and wait for the delivery goroutine before native UI exit
- [x] 1.5 Add reconciliation to the notification service: after the sink recovers or drops occurred, re-derive active call, missed call, and recent SMS state from the extras and SMS services and re-issue `ShowCall`/`HideCall`/`ShowMissedCall`/`ShowSMS` so the UI converges (e.g., the incoming-call card closes after hangup)

## 2. Native UI Command Delivery (D3)

- [x] 2.1 On Swift-to-Go queue rejection in `internal/platform/darwin/native/bridge.go`, emit a Go-to-Swift `command.dropped` event carrying command name and reason, and log the drop on the Go side; do not silently discard or report the command as executed
- [x] 2.2 Add a bounded timeout (5-10 s) to the Swift `rejectingCallID` state in `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift` that clears the rejecting state and restores the buttons when no reject result arrives; make `callRejectFailed` clear state regardless of call-ID match

## 3. Shutdown Ordering (D4)

- [x] 3.1 Add the notification-style `Stop` (cancel + done channel, idempotent) to the SMS polling service (`internal/application/sms/service.go`) and have `Start` store them
- [x] 3.2 Add the notification-style `Stop` to the network polling service (`internal/application/network/service.go`, both pollers)
- [x] 3.3 Add the notification-style `Stop` to the extras service including its internally started call poller (`internal/application/extras/service.go`)
- [x] 3.4 Add an idempotent shutdown-admission gate before HTTP draining; rework `App.Stop(ctx)` (`internal/app/app.go`) to close admission, call `Operations.Shutdown(ctx)`, then stop Notification, Extras, Network, SMS, and Runtime in that reverse start order, wait for each worker to join, and close the store last; preserve the native UI until all sink calls have returned
- [x] 3.5 Add `Shutdown(ctx)` to `operation.Manager` (`internal/application/operation/manager.go`): mark the manager closed, change `Start` to return `(string, error)`, reject new work with a structured shutdown/unavailable error, cancel every tracked operation via the `cancels` map, wait for `run` goroutines within the bounded context, and make repeated calls idempotent without duplicate cancellation; verify cancelled workers report the `Cancelled` terminal state
- [x] 3.6 Thread the request context through the eSIM read paths (`internal/esim/manager.go` overview/EID/profiles and `internal/esim/at_port.go`) and check `ctx.Err()` between per-AID APDU steps so a cancelled request stops promptly and releases `opMu`/the arbiter; `at_port.go` also gains device-arbiter constructor wiring in `cleanup-architectural-debt` D10 — disjoint edits, coordinate order but do not touch each other's code

## 4. Single Shutdown Path (D5)

- [x] 4.1 Remove the concurrent `<-ctx.Done()` goroutine in the HasUI branch of `cmd/djonehub/main.go`; close shutdown admission before draining the HTTP server, then run a single goroutine selecting on UI-exit and `ctx.Done()` that executes the one bounded shutdown sequence and preserves separate drain/worker deadlines
- [x] 4.2 Gate `Bridge.send` on the existing started/exited state so no event is posted to a stopped AppKit run loop (`internal/platform/darwin/native/bridge.go`)
- [x] 4.3 Bind the HTTP listener with `net.Listen` before starting the native UI; return serve errors to the main flow instead of `log.Fatal` from the goroutine, so a bind/serve failure runs the normal shutdown path and the UI is not started against an unreachable URL

## 5. Events WebSocket Migration (D6)

- [x] 5.1 Replace the hand-written upgrade in the events handler (`internal/api/http/server.go`) with `websocket.Upgrader` using `github.com/gorilla/websocket` (already present in `go.mod`; no dependency change needed), preserving the temporary loopback Origin/Host checks from `fix-security-and-data-loss`
- [x] 5.2 Add a read loop with `SetReadDeadline` and a pong handler, plus `SetWriteDeadline` on writes and periodic pings, so stale or silent clients are closed and unsubscribed; serialize all event and ping writes through one writer
- [x] 5.3 Add `EventBus.SubscribeWithWatermark(buffer)` returning the subscription, captured watermark, drop counter, and idempotent unsubscribe; capture subscription and sequence under the bus lock, send the device snapshot with that watermark, then forward every queued event with `ID > watermark`, and close an overflowing WebSocket subscription to force client resync
- [x] 5.4 Confirm the `Event` JSON envelope, `snapshot` event type, and `publicEvent`/`publicDeviceStatus` sanitization are preserved through the migration (gorilla fragments oversized frames, removing the >65535-byte silent drop)

## 6. VoWiFi Lifecycle Convergence (D7)

- [x] 6.1 Rework `Host.fail` in `internal/vowifihost/host.go` to cancel the stored child context and close any opened port before setting `Failed`, so repeated failed enables cannot leak modem ports or event consumers
- [x] 6.2 Serialize `Enable`/`Disable`/`Reconnect`/`Recover`/`DeviceRemoved` through one transition lock or actor so no two transitions interleave on the same port
- [x] 6.3 Replace the per-event `go Recover(context.Background())` calls in the application `followRuntime` and host `consumeEvents` with a single-flight, debounced recovery trigger, and run recovery under the `ResourceVoWiFi` lock
- [x] 6.4 Tie the `followRuntime` subscription to the session lifecycle in `internal/application/vowifi/service.go`: retain the unsubscribe and exit the goroutine when the session context ends

## 7. Logger Wiring (D8)

- [x] 7.1 Call `logger.Setup` in `cmd/djonehub/main.go` before `app.New`/`instance.Start`, deriving `LogConfig` from the existing CLI flags, so zap logs from modem, backend, and eSIM layers are emitted instead of discarded by the Nop logger

## 8. Verification

- [x] 8.1 Add tests: event-bus and backend drop counters increment without blocking producers; a slow subscriber never stalls the AT command loop; `GET /api/v1/notifications/debug` exposes cumulative/active counters and unsubscribe removes only active-subscriber state
- [x] 8.2 Add notification tests: slow sink does not block event consumption; after recovery the service re-derives call/SMS state so the call card closes and missed-call/SMS prompts are re-issued
- [x] 8.3 Add tests for reject-call timeout behavior (Swift unit test or bridge-level test with a missing result) and for `command.dropped` feedback when the Swift-to-Go command queue is full
- [x] 8.4 Add shutdown tests: admission closes before HTTP drain; `App.Stop(ctx)` cancels operations, joins Notification, Extras, Network, SMS, and Runtime in reverse start order before closing the store (no "database is closed" write race); `Start` returns a structured error and no ID after closure; repeated `operation.Shutdown(ctx)` is safe; a caller timeout does not poison later shutdown waits; sink queue timeout aborts queued calls without calls after native UI stop; eSIM read cancellation stops mid-scan
- [x] 8.5 Add WebSocket tests: stale session is reclaimed after missed pings; a publish during snapshot construction is delivered after the snapshot using the captured watermark; event and ping writes do not race
- [x] 8.6 Add vowifihost tests: failed enable cleans up cancel/port across retries; concurrent recovery triggers collapse into one debounced run; recovery cannot interleave with user Enable/Disable
- [x] 8.7 Add a logger test that after `logger.Setup` a device-layer log statement reaches the configured output
- [x] 8.8 Run `go test -race` over runtime, backend, application, vowifihost, esim, and api packages and fix any reported races
- [x] 8.9 Smoke-test on macOS: quit via menu bar and via SIGTERM both run one clean shutdown; reject-call button recovers after a dropped command; device-layer logs appear in the log output (requires a device and manual UI interaction)
