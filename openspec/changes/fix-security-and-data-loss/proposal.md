## Why

The local HTTP control plane binds to loopback by default but has no cross-site protections: WebSocket upgrades skip Origin checks, and cross-site `text/plain` simple requests can drive write endpoints, including sending SMS, deleting eSIM profiles, and executing arbitrary AT commands. Login authentication is deliberately deferred; until its first-use flow is designed, the server SHALL accept only loopback-form bind addresses (`127.0.0.1`, `localhost`, `::1`). At the same time, several P1 correctness defects silently lose data: inbound SMS are read-and-deleted with no consumer registered, multipart SMS never reassemble across refresh polls, AT timeouts leak stale responses into the next command, eSIM write completion can be falsely reported as "operation in progress", device-controlled counts can exhaust memory during parsing, and a notification-click cold start can abort the macOS process under Swift 6 actor isolation. These are the first-priority items from the full-repository code review and must be fixed before any further work.

## What Changes

- Enforce the temporary local boundary: reject every listen address whose host is not a loopback form (`127.0.0.1`, `localhost`, `::1`), validate Origin/Host on all WebSocket upgrades (event stream and ADB shell), and reject cross-site simple write requests via Content-Type/Sec-Fetch-Site/Origin checks. Login authentication, credential storage, and credential-bearing clients are explicitly deferred.
- Repair the SMS pipeline: register an application-layer consumer for inbound SMS with durable-persistence-before-acknowledgement semantics (conflict-ignored inserts count as persisted), preserve the modem's true storage indices through list/read/delete, decode PDU content inside the backends, persist multipart reassembly state across refresh cycles, treat the startup refresh as a baseline that does not re-publish retained messages, serialize storage-selection switching, and include the total segment count in reassembly keys.
- Make AT response handling deterministic: drain residual responses after a timeout so they cannot be consumed by the next command, recognize prompts only as bare prompt text during interactive commands, dispatch URCs before prompt detection, and make the consecutive-timeout watchdog threshold configurable.
- Fix eSIM synchronization primitives: make write-completion notification race-free, notify a lease holder on force-release, and do not grant new exclusive leases or SIM-switch barriers while a force-released lease's APDU is still in flight.
- Bound protocol parsing: validate device-controlled counts and lengths against the remaining buffer before preallocation, cap fragment-collector accumulation with cleanup on cancellation, and cap modem line buffering.
- Fix macOS notification delegate callbacks: make the `UNUserNotificationCenter` delegate methods `nonisolated` so Swift 6 main-actor isolation checks cannot abort the process on notification-click cold start.

## Capabilities

### New Capabilities

- `macos-native-ui`: macOS notification and menu-bar UI delivery behavior, including actor-safe notification delegate callbacks and correct panel lifecycle.

### Modified Capabilities

- `device-api`: the temporary loopback-only boundary (accepting `127.0.0.1`, `localhost`, `::1`) is enforced with Origin/Host validation and cross-site write rejection; login authentication remains deferred.
- `device-events`: WebSocket upgrades gain loopback Origin/Host validation before any event stream is established; authentication is deferred.
- `modem-backends`: SMS list/read/delete gains true storage indices and PDU decoding with consumer-owned inbound delivery; AT timeout handling gains quarantine and precise prompt semantics; APDU coordination becomes race-free and exclusive under force-release with transport quarantine owned by the transport owner; device-controlled data parsing becomes bounded.
- `device-services`: SMS reassembly becomes persistent across refresh cycles, inbound SMS delivery becomes consumer-owned with durable-persistence-before-acknowledgement, and the startup refresh establishes a baseline without re-publishing retained messages.

## Impact

- Go: `internal/app`, `internal/api/http`, `cmd/djonehub`, `internal/modem`, `internal/backend`, `internal/application/sms`, `internal/esim`, `internal/apduarbiter`, `pkg/mbim`, `pkg/smscodec`.
- macOS Swift: `macos/DJOneHubNotifier` (notification delegate).
- Web: no credential changes; existing REST and WebSocket clients remain same-origin loopback clients.
- OpenAPI: no deferred login security scheme is declared.
