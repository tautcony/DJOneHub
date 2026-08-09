# Diagnostic Code Map

Use this map to find the first code and test for a visible fault. First confirm the API state. Do not change a real device unless the user authorizes the action.

## Process and HTTP

| Symptom | First source files | First tests or commands |
| --- | --- | --- |
| The process does not listen | `cmd/djonehub/main.go` | `go test ./cmd/djonehub`, `./scripts/dev.sh -demo` |
| The UI gives an API error | `web/src/services/api.ts`, `internal/api/http/server.go` | `go test ./internal/api/http` |
| A write request is rejected | `server.go`, `internal/api/http/boundary.go` | `boundary_test.go`, `server_test.go` |
| The API stops during shutdown | `main.go`, `app.go`, `operation/manager.go` | `app_stop_test.go`, `shutdown_test.go` |
| The event socket does not connect | `server.go`, `web/src/stores/device.ts` | `websocket_test.go`, `runtime_stream_test.go` |

## Device Discovery and Capability

| Symptom | First source files | What to confirm |
| --- | --- | --- |
| No device appears | OS adapter, `runtime/runtime.go` | USB identity, serial or control path, discovery result |
| The wrong device identity appears | OS adapter, `domain/device/` | Stable ID, physical location, VID:PID, and IMEI |
| A device reconnects as a new device | OS adapter, `runtime/runtime.go` | Stable-ID construction and candidate comparison |
| A feature is hidden or disabled | `backend/business_adapter.go`, `device.ts` | Capability snapshot, ready state, and route rule |
| A feature reports unsupported | Related backend port and application service | Actual backend support and structured error code |
| A rescan has no effect | `runtime/runtime.go`, `server.go` | Shared scan path, session state, and discovery output |

For macOS discovery, read `internal/platform/darwin/adapter.go`. The accepted VID:PID values are `2ca3:4006` and `2c7c:0125`.

## Events and Operations

| Symptom | First source files | What to confirm |
| --- | --- | --- |
| The initial page state is wrong | API device handler, device service | Status snapshot and service response |
| A page becomes stale | `runtime/events.go`, `device.ts` | Socket state, event ID, event type, and domain handler |
| An operation has no progress | Operation manager, feature service | `operation.progress` and operation ID |
| An operation never becomes final | Operation manager, feature service | Context cancellation and `operation.completed` |
| A notification is missing | Notification service, native bridge | Event baseline, preferences, sink, and permission |

Use `/api/v1/runtime/diagnostics` and `/api/v1/runtime/traces` before you add new logging. Use `/api/v1/notifications/debug` for notification state.

## Feature Faults

| Symptom | First source files | Primary tests |
| --- | --- | --- |
| SMS list is empty or old | `application/sms/service.go`, backend SMS port | `consumer_test.go`, `merge_test.go` |
| A long SMS is wrong | `pkg/smscodec/` | SMS merge tests and package tests |
| A call action is unavailable | `application/extras/service.go`, backend capability | `dial_test.go`, `events_test.go` |
| The eSIM overview fails | `application/esim/service.go`, `esim/manager.go` | eSIM service and manager tests |
| A profile action conflicts | `esim/manager.go`, `apduarbiter/arbiter.go` | Arbiter and eSIM tests |
| A local profile note is absent | SIM-profile service and store | SIM-profile and storage tests |
| Network state is wrong | Network service and platform adapter | Network and platform tests |
| VoWiFi is not available | VoWiFi service, backend capability, packet tunnel | `internal/vowifihost/*_test.go` |
| A firmware action fails | Firmware service, raw AT service, platform adapter | Firmware service and ADB tests |

Firmware, USB mode, eSIM, and raw AT actions can change device state. Use read-only API checks first. Get user authorization before you run a device-changing action.
