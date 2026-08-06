## Why

The second review batch (docs/code-review-report.md items 7-10) targets reliability and lifecycle defects that become visible under sustained use: events and notifications are silently dropped when any consumer is slow, shutdown never stops or joins the polling services so the database can be closed underneath in-flight writes, in-flight operations cannot be cancelled, VoWiFi recovery spawns unbounded concurrent restarts that leak modem ports, and the device-layer zap logger is never initialized so all modem/backend/eSIM logs are discarded. These failures do not crash the process; they corrupt state, strand the UI in stuck states, and make the long-running device agent unreliable.

## What Changes

- Make event and notification delivery non-blocking and accountable: `EventBus.Publish` and backend drops are counted and exposed through an additive `event_drops` object on the existing notification-debug response; backend event channels (AT/QMI) never stall the command loop on a slow consumer; the notification consumer decouples its synchronous Swift-sink calls into an internal queue with a dedicated delivery goroutine and reconciles call/SMS state after the sink recovers.
- Make native UI delivery self-recovering: Swift-to-Go bridge command queue rejections are reported back as Go-to-Swift `command.dropped` events, and the Swift reject-call state recovers by timeout instead of leaving the UI stuck in "rejecting".
- Complete shutdown ordering: SMS, network, and extras polling services become stoppable and joinable (mirroring the notification service's `Stop` pattern); shutdown prevents new work, cancels in-flight operations, stops Notification, Extras, Network, SMS, and Runtime in reverse start order, and joins them before closing the store; device read paths honor context cancellation; the UI and signal paths converge on a single shutdown path that does not deliver events to a stopped UI; `ListenAndServe` failure returns to the main flow instead of `os.Exit` while the UI still starts against a dead URL.
- Migrate the events WebSocket to gorilla/websocket with a read loop and read/write deadlines so stale clients are reclaimed, and fix snapshot ordering so a session subscribes before the snapshot is sent and snapshot IDs are monotonic (no missed events under client-side deduplication).
- Converge VoWiFi lifecycle control: `Enable`/`Disable`/`Reconnect`/`Recover` are serialized through one state owner, failure paths clean up the cancel function and any opened port, recovery is single-flight and debounced, and the runtime-event subscription is tied to the session context.
- Wire `logger.Setup` into the application entry point so device-layer zap logging (modem, backend, eSIM) is emitted instead of being discarded by the Nop logger.

## Capabilities

### New Capabilities

- `device-logging`: structured application and device-layer logging initialized at startup, replacing the Nop logger so device-layer log output is actually emitted.
- `macos-native-ui` (no baseline; introduced by `fix-security-and-data-loss`, extended here): notification delivery to the native UI is decoupled from event consumption and reconciled after recovery; user-critical bridge commands are not silently dropped; reject-call state recovers by timeout.

### Modified Capabilities

- `device-events`: event publishing becomes non-blocking with drop accounting; WebSocket sessions gain read-loop and deadline hygiene; the snapshot requirement is merged into one authoritative block here (loopback/auth-deferred boundary + subscribe-with-watermark ordering) and the sibling's duplicate MODIFIED block is removed.
- `modem-backends`: backend event channels become non-blocking with drop accounting so a slow consumer cannot stall the AT command loop or QMI event dispatch.
- `single-device-runtime`: the application shuts down through one waitable path that stops and joins polling workers in reverse start order before closing storage, cancels in-flight operations, and never delivers events to a stopped UI.
- `device-services`: long-running operations become cancellable (a shutdown cancel of all in-flight operations) and device read paths honor context cancellation.
- `vowifi-lifecycle`: VoWiFi control transitions are serialized through one state owner with complete failure cleanup, and recovery is single-flight and debounced.

## Impact

- Go: `internal/runtime` (event bus), `internal/backend` (AT/QMI event channels), `internal/application/notification` (sink decoupling), `internal/application/operation` (shutdown), `internal/application/{sms,network,extras}` (stoppable pollers), `internal/app` and `cmd/djonehub` (single shutdown path, `logger.Setup`), `internal/api/http` (WebSocket migration), `internal/platform/darwin/native` (bridge drop feedback), `internal/vowifihost` (state machine), `internal/esim` (read-path cancellation), `pkg/logger` (initialization entry point).
- macOS Swift: `macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift` (reject-call timeout).
- Dependencies: `github.com/gorilla/websocket` (already present in `go.mod`) gains its first use in the events WebSocket migration; no new endpoints or storage schema changes, with additive drop counters on the existing notification-debug diagnostics response.
