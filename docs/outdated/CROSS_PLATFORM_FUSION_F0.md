# DJOneHub Cross-Platform Fusion F0

This document is the implementation trace for the `cross-platform-fusion` change.

## Source and reuse boundary

| Source | Commit | License | Reusable scope |
| --- | --- | --- | --- |
| DJOneHub repository | `7025f1cf0216fef22c62b7696d948a57a2837265` | PolyForm Noncommercial 1.0.0 | Existing `internal/backend`, `internal/modem`, `internal/esim`, `internal/apduarbiter`, `pkg/mbim`, `pkg/smscodec`, and current macOS compatibility behavior |
| `vohive-open` reference tree | `de689a554d1b86b97dcc71140bfbee250eff1d4e` | PolyForm Noncommercial 1.0.0 | Architecture and implementation patterns only; no product-surface copy without an explicit DJOneHub requirement |

The required notice from both source trees remains in `LICENSE` and `THIRD_PARTY_NOTICES.md`. New code in this change is DJOneHub code and must remain within the repository license policy. The allowed reference directories are backend/application boundaries, `internal/vowifihost` lifecycle patterns, protocol packages, and Vue build patterns. The full `vohive-open/web` application, unrelated services, and product pages are excluded.

## Explicitly out of scope

- Device pools, multi-device scheduling, leases, or a device registry.
- Proxy pools, proxy instances, upstream proxy orchestration, or country routing.
- Notification channels, bots, code-receiving centers, and remote operations.
- Remote multi-tenant administration, distributed jobs, or operator statistics.
- The complete VoHive management web application and unrelated settings/pages.
- Unverified macOS or Windows VoWiFi data-plane support.

The single-device runtime owns one physical device. A future multi-device feature requires a separate change and must not be smuggled into these interfaces.

## Domain vocabulary

### Device lifecycle

`absent`, `discovered`, `connecting`, `initializing`, `ready`, `degraded`, and `disconnected` are the only lifecycle states. The runtime is the sole owner of the current lifecycle state.

### Stable identity

`StableDeviceID` is derived from the strongest available ordered inputs: persistent serial number, IMEI, physical location, and VID/PID. A re-enumerated device may change ports while retaining its stable ID when identity inputs still match.

### Backend mode

`at`, `qmi`, and `mbim` identify protocol backends. The selected mode is accompanied by a human-readable selection reason and a capability set.

### Capabilities

Capability names are lower snake case and stable across transports. The initial vocabulary is:

`device_status`, `raw_at`, `sms_read`, `sms_send`, `sim`, `apdu`, `esim`, `ussd`, `network_status`, `network_control`, `vowifi_inspect`, `vowifi_control`, and `packet_tunnel`.

Unsupported operations must identify the missing capability rather than infer support from the platform name or backend type.

## Error contract

All application and API failures use a stable code, message, retryability, and optional details map. Initial codes are:

`invalid_request`, `unauthenticated`, `not_found`, `device_offline`, `operation_conflict`, `operation_cancelled`, `operation_timeout`, `backend_unavailable`, `transport_unavailable`, `capability_not_supported`, `packet_tunnel_not_supported`, and `internal_error`.

`capability_not_supported` details include `capability`, `operation`, and optionally `reason`. Retryability is false for invalid input, authentication failures, unsupported capabilities, and conflicts caused by a caller; it is true for transient device, transport, backend, and timeout failures when retrying is meaningful.

The HTTP mapping is 400 for invalid requests, 401 for authentication, 404 for missing resources, 409 for conflicts, 422 for unsupported capabilities, 503 for unavailable devices/transports, 504 for timeouts, and 500 for unknown failures.

## REST v1 contract

The local API is rooted at `/api/v1`:

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/device/status` | Current single-device snapshot |
| POST | `/device/actions/rescan` | Request discovery/rescan |
| POST | `/device/actions/raw-at` | Execute raw AT when supported |
| GET/POST | `/sms`, `/sms/actions/send`, `/sms/actions/refresh`, `/sms/actions/clear` | SMS workflows |
| GET/POST | `/esim`, `/esim/actions/*` | eSIM inspection and operations |
| GET/POST | `/network`, `/network/actions/*` | Network status, mode, traffic, diagnostics |
| GET/POST | `/vowifi`, `/vowifi/actions/*` | VoWiFi status and lifecycle commands |
| GET | `/openapi.json` | Machine-readable contract |
| GET | `/events/ws` | Authenticated event stream |

Long-running commands return `{ "operation_id": "..." }`. Errors return `{ "error": { "code", "message", "retryable", "details" } }`.

## WebSocket event contract

Each event is `{ "id", "type", "version", "occurred_at", "data" }`. Event IDs are monotonically increasing per runtime. The first message on every connection is a `snapshot` containing the complete device and capability view. Clients resynchronize by requesting the snapshot when an event ID is missing or out of order.

The initial event types are `device.status.changed`, `backend.changed`, `sim.changed`, `sms.updated`, `esim.updated`, `network.updated`, `vowifi.updated`, `operation.progress`, and `operation.completed`.

## Existing macOS entry inventory

The current `cmd/djonehub-macos/main.go` responsibilities map as follows:

| Existing functions | Target owner |
| --- | --- |
| `main`, `serve`, `routes`, `health` | `cmd/djonehub`, `internal/app`, API adapter |
| `status`, `statusPayload`, USB identity helpers | device domain/application and macOS platform adapter |
| `runATCommand`, `runATOK`, SMS polling/send/cleanup | modem backend and SMS application service |
| eSIM overview, health, profile, APDU, note handlers | eSIM application service and storage |
| network diagnostic, traffic, route checks, USB mode | network application service and macOS network adapter |
| USB discovery/open/reset/detach | discovery and serial/USB transport ports |
| embedded `web/*` handlers and page assets | compatibility entry during Vue migration |

No target package may call the old HTTP handlers to obtain domain state. Compatibility routes may call application services once those services are in place.
