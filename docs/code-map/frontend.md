# Frontend Code Map

## Entry Path

`web/src/main.ts` creates the Vue application. `web/src/App.vue` composes the application shell and the view state. `web/src/router.ts` defines the navigation entries.

`cmd/djonehub/main.go` serves the production files from `web/dist`. The development scripts start Vite and proxy API and WebSocket requests to the Go server.

## Navigation and Capability Rules

`web/src/router.ts` maps a `ViewID` to a browser path and a lazy-loaded view. `web/src/App.vue` decides which navigation entries are visible.

`web/src/App.vue` keeps all supported navigation entries visible. Feature views use the device capability snapshot to enable, disable, or explain their concrete actions. `web/src/stores/appearance.ts` owns the light, dark, and system appearance preference.

The server capability snapshot controls feature actions. Navigation stays visible so users can inspect unavailable features. Do not use a browser OS check to select a modem feature.

Use this read order when a page is missing or cannot open:

1. Read `web/src/router.ts` for the path and component.
2. Read the navigation definition in `web/src/App.vue`.
3. Read `web/src/stores/device.ts` for `has` and the current snapshot.
4. Read `internal/api/http/server.go` and the related application service.

## Module Owners

| Path | Owner | Main responsibility |
| --- | --- | --- |
| `web/src/App.vue` | Application composition | View loading, shell state, and cross-view actions |
| `web/src/router.ts` | Navigation | View entries and capability requirements |
| `web/src/services/` | API access | Typed HTTP requests, AT helpers, and eSIM QR input |
| `web/src/stores/` | Domain state | Device, SMS, eSIM, network, SIM profile, and VoWiFi state |
| `web/src/views/` | Feature UI | Feature-specific pages and settings sections |
| `web/src/components/` | Shared UI | Shell, status, panel, loading, and operation components |
| `web/src/types.ts` | API types | Shared frontend models |
| `web/src/i18n.ts` | Text resources | Chinese and English UI text |
| `web/src/utils/` | UI helpers | Date, format, and SIM-profile helpers |

## Data Flow

The frontend gets the device snapshot from the API. It then keeps the state current from the WebSocket event stream.

```text
Vue view
    |
store or view action
    |
web/src/services
    |
/api/v1 and /api/v1/events/ws
    |
Go HTTP server
```

The server capability snapshot controls feature actions. Navigation stays visible when a capability is unavailable. Do not select behavior from the browser platform or from a guessed modem state.

Long-running actions return an operation ID. Show the operation state until it is final. When the event sequence has a gap, get a new snapshot.

The Device Control view renders EDL facts from the server response and disables device mutations while a device-control operation is active (its own operation, or `active_operation` reported by the server for another client). Device mutual exclusion is enforced server-side through the busy state; the view has no client token or lease.

The `device_control.edl_session_changed` event schedules a fresh Device Control status request. The event payload is not treated as an unmasked UI source.

`device.ts` handles these conditions:

- A non-ready snapshot keeps the device identity but clears usable capabilities.
- A WebSocket ID gap gets a new status snapshot.
- A duplicate or old event is ignored.
- A final operation stays visible for five minutes, then the store removes it.
- A socket failure uses bounded exponential reconnect delay with random variation.

Feature stores receive domain events through `registerDomainHandler`.

## Primary Checks

Run these commands after frontend changes:

```sh
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run format:check
npm --prefix web run test
npm --prefix web run build
```

Focused frontend tests are under `web/src/**/*.test.ts`. Keep pure appearance logic covered without requiring a device or a backend.

The GitHub workflow `.github/workflows/frontend.yml` runs the frontend checks on push and pull request.

## Frontend Fault Checks

| Symptom | First files | Likely boundary |
| --- | --- | --- |
| The page does not load | `router.ts`, `App.vue` | Route, navigation rule, or view load |
| The page shows no data | Feature store, `api.ts` | API call or initial load |
| Data does not refresh | `stores/device.ts` | WebSocket, event type, or domain handler |
| A button is disabled | `App.vue`, feature view, `device.has` | Capability snapshot or ready state |
| The action starts but never ends | `device.ts`, operation component | Missing operation event or API status |
| Text is wrong or missing | `i18n.ts`, view | Missing translation key or wrong text group |
| Sensitive data is visible | View, `utils/simprofiles.ts` | Missing masking rule |
