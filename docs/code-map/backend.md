# Backend Code Map

## Entry Path

`cmd/djonehub/main.go` is the product entry point. It does these actions:

1. It locks the main OS thread for the macOS native UI.
2. It creates `internal/app.App`.
3. It starts the device runtime and the HTTP server.
4. It serves `web/dist` for non-API paths.
5. It runs the ordered shutdown path.

`internal/app/app.go` is the composition root. It selects the platform adapter. It creates the runtime, services, storage, notification sinks, native UI bridge, and HTTP server.

## Startup and Shutdown

Read `cmd/djonehub/main.go` first for a process startup or shutdown problem. The file locks the main OS thread, reads `-listen`, `-web-dir`, and `-demo`, starts `internal/app.App`, and starts the HTTP server.

`internal/app/app.go` is the only composition root. `app.New` selects the platform adapter. `newApp` creates the SQLite store, runtime, operation manager, application services, notification sinks, native UI bridge, and HTTP server.

`App.Start` starts the runtime, SMS poller, network poller, calls service, notification service, remote channels, and VoWiFi service. `App.BeginShutdown` stops new HTTP work and operations. `App.Stop` stops services in reverse dependency order.

Read `cmd/djonehub/main_test.go` and `internal/app/app_stop_test.go` before you change startup or shutdown.

## Device Runtime

`internal/runtime/runtime.go` owns one device session. It runs device discovery at a fixed interval. An HTTP rescan uses the same serialized scan path.

The runtime owns the active candidate, backend, snapshot, event bus, resource locks, scan lock, session context, and worker group.

`scanMu` serializes timer scans, HTTP rescans, disconnects, and stop. Do not start another scan path outside `Runtime.scan`.

Read these tests for a runtime fault:

| Problem | Tests |
| --- | --- |
| State or event behavior | `internal/runtime/events_test.go`, `internal/runtime/runtime_test.go` |
| Concurrent scan or stop | `internal/runtime/scan_serialization_test.go` |
| HTTP rescan behavior | `internal/api/http/server_test.go`, `internal/api/http/shutdown_test.go` |

## Backend Selection and Transport

`internal/backend/selector.go` selects QMI, MBIM, or AT. The default selection order is QMI, MBIM, then AT.

`internal/backend/factory.go` and `internal/backend/at_factory.go` create backend instances. `internal/backend/contracts.go` defines shared backend ports. `internal/backend/business_adapter.go` changes backend ports into application capabilities.

| Symptom | First files |
| --- | --- |
| A backend is not selected | `internal/backend/selector.go`, platform adapter |
| A feature gives `capability_not_supported` | `internal/backend/contracts.go`, `internal/backend/business_adapter.go` |
| An AT command or unsolicited result code is wrong | `internal/domain/at/command.go`, `internal/modem/manager.go`, `internal/modem/transport.go`, `internal/modem/commands.go`, `internal/modem/at_parse.go` |
| A QMI or MBIM result is wrong | `internal/backend/qmi_backend.go` or `internal/backend/mbim_backend.go` |
| An APDU action conflicts | `internal/apduarbiter/arbiter.go`, `internal/esim/apdu_coordinator.go` |

## Module Owners

| Path | Owner | Main responsibility |
| --- | --- | --- |
| `internal/domain/` | Domain model | Device models, capabilities, and typed errors |
| `internal/runtime/` | Runtime | One-device lifecycle, events, locks, and scans |
| `internal/backend/` | Modem backend | Shared AT, QMI, and MBIM contracts and adapters |
| `internal/modem/` | Modem control | Shared AT session, commands, URCs, and modem-specific behavior |
| `internal/application/` | Use cases | Device, SMS, eSIM, network, AT, VoWiFi, device control, calls, and operations |
| `internal/app/app.go` | Runtime wiring | Inject verified EDL observation ports by capability |
| `internal/esim/` | eUICC access | APDU transport, channels, profile actions, and recovery |
| `internal/apduarbiter/` | APDU access | Device-level APDU leases, barriers, and readiness |
| `internal/platform/` | Platform adapter | Device discovery, transport, network, startup, and native UI support |
| `internal/api/http/` | HTTP boundary | `/api/v1` routes, WebSocket events, input checks, and error responses |
| `internal/storage/` | Local storage | SQLite data and JSON migration support |
| `internal/notify/` | Remote notices | Notification channel settings and delivery |
| `pkg/` | Shared packages | MBIM protocol, SMS codec, logging, and SMS utilities |

`internal/domain/at/command.go` maps an AT command to a fixed diagnostic domain. Modem logs and Raw AT events use the same mapping. The mapping must not return command names or arguments.

## Runtime Wiring

The normal request and event paths are:

```text
HTTP or WebSocket client
        |
internal/api/http
        |
internal/application
        |
internal/runtime and internal/backend
        |
internal/platform and device transport
```

`runtime.Events()` sends snapshots and ordered events to the HTTP server. It also sends events to `internal/application/notification.Service`.

The notification service sends native events to `internal/platform/native` and remote events to `internal/notify`.

`internal/app/app.go` is the only place that joins these owners. Do not move platform or HTTP behavior into an application service.

## Primary Tests

| Area | Primary tests |
| --- | --- |
| Startup and shutdown | `cmd/djonehub/main_test.go`, `internal/app/app_stop_test.go` |
| HTTP boundary and event stream | `internal/api/http/*_test.go` |
| Runtime lifecycle and events | `internal/runtime/*_test.go` |
| Backend selection and capability behavior | `internal/backend/*_test.go` |
| eSIM and APDU access | `internal/esim/*_test.go`, `internal/apduarbiter/*_test.go` |
| Use-case services | `internal/application/*/*_test.go` |
| Storage migration and data | `internal/storage/*_test.go` |

Run `go test ./...` after Go changes. Use `go test -race` when work changes synchronization or lifecycle code.

## Operations

`internal/application/operation/manager.go` owns asynchronous operation state. The states are `pending`, `running`, `succeeded`, `failed`, and `cancelled`.

The manager publishes `operation.changed`, `operation.progress`, `operation.log`, and `operation.completed`. An operation has a detached request context. Application shutdown cancels active operations.

Read `internal/application/operation/manager_test.go` before you change operation state, progress, cancellation, or shutdown behavior.
