# API, Event, and Data Map

## HTTP Boundary

`internal/api/http/server.go` owns the local HTTP boundary. `Server.Handler` registers all `/api/v1` routes. Read this file first when a request has a wrong method, route, response, or error.

`cmd/djonehub/main.go` limits the listener to a loopback address. `Server.SetLoopbackPort` gives the HTTP server the permitted port for Origin and Host checks.

The HTTP server checks these items before it calls a service:

- The request method
- The loopback Origin and Host boundary
- The JSON request body for command routes
- The application admission gate
- The required capability

Read `internal/api/http/boundary_test.go`, `server_test.go`, `sanitize_test.go`, and `websocket_test.go` when you change this boundary.

## Route Groups

| Route group | Application owner | First source |
| --- | --- | --- |
| `/device`, `/runtime` | Device service and runtime | `internal/runtime/` |
| `/sms` | SMS service | `internal/application/sms/` |
| `/esim`, `/sim-profiles` | eSIM and SIM-profile services | `internal/application/esim/`, `internal/application/simprofiles/` |
| `/network` | Network service | `internal/application/network/` |
| `/device/actions/raw-at` | Raw AT service | `internal/application/rawat/` |
| `/device-control` | Device-control service | `internal/application/firmware/` |
| `/vowifi` | VoWiFi service | `internal/application/vowifi/` |
| `/calls` | Extras service | `internal/application/extras/` |
| `/notifications`, `/settings/startup` | Notification and startup services | `internal/application/notification/`, `internal/platform/startup/` |
| `/operations/{id}` | Operation manager | `internal/application/operation/` |
| `/events/ws` | Runtime event bus | `internal/runtime/events.go` |

`/api/v1/openapi.json` gives the generated route description. Read `internal/api/http/openapi.go` when you change API documentation.

## Error and Capability Rules

The API error object has `code`, `message`, `retryable`, and optional `details` fields.

Use a typed domain error from `internal/domain/errors/`. Do not make a client depend on error text.

The capability snapshot is the authority for a feature. A platform or backend must not report a capability until it can perform that feature. An unsupported action must return `capability_not_supported`.

Read these files before you change API or capability behavior:

- `openspec/specs/device-api/spec.md`
- `openspec/specs/modem-backends/spec.md`
- `openspec/specs/platform-adapters/spec.md`

## Event Stream

`internal/runtime/events.go` defines the event bus. The WebSocket server sends a full snapshot first. Later events have increasing runtime IDs.

The frontend entry is `web/src/stores/device.ts`. A missing event ID causes a new device-status request. A duplicate or old event is ignored.

| Event group | Producer | Main consumer |
| --- | --- | --- |
| `snapshot`, `device.status.changed` | Runtime | HTTP server and device store |
| `operation.*` | Operation manager | Device store and feature views |
| `sms.*` | SMS service | SMS store and notification service |
| `network.*` | Network service | Network store and notification service |
| `call.*` | Extras service | Calls view and notification service |
| eSIM profile and confirmation events | eSIM service | eSIM store and view |
| Notification events | Notification service and bridge | Native UI and notifications view |
| `device_control.edl_session_changed` | EDL session manager | Device Control view and WebSocket observers |

Read `internal/api/http/runtime_stream_test.go`, `internal/api/http/websocket_test.go`, and `internal/runtime/events_test.go` for an event problem.

## Local Data

`internal/storage/sqlite.go` opens the SQLite store. `internal/app/app.go` selects the user configuration directory and passes store namespaces to services.

| Data | Owner |
| --- | --- |
| Notification preferences | Notification service namespace |
| Remote notification channels | Notify manager namespace |
| SMS sent history and local cache | SMS service |
| SIM profile labels, notes, and tags | SIM-profile service |
| eSIM notification history | eSIM service |
| Network traffic data | Network service |
| Device-control settings | Device-control service |

The eUICC owns card-side profile data. SQLite owns local names, notes, and tags. Do not merge these sources in code or UI.
