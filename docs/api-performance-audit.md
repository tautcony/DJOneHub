# API Performance Audit

## Scope

This audit covers the 69 HTTP route registrations in `internal/api/http/server.go`.
It also covers the application services, backend adapters, AT command manager,
SQLite store, browser request patterns, and background pollers that affect API
latency.

The audit uses commit `31cf052` as its implementation baseline. It does not use
real-device writes or destructive verification. Hardware latency targets in this
document are provisional. Validate them on each supported platform, backend, and
device class.

## Executive Summary

The current implementation has useful cache foundations:

- Device identity, radio, and SIM snapshots use a 5-second, runtime-generation
  cache. Concurrent cold reads are coalesced per snapshot type.
- The eSIM overview uses a 10-second, runtime-generation cache. Concurrent cold
  reads are coalesced. The eSIM manager also retains discovered AIDs and chip
  information until a validated invalidation event.
- Device Control status uses a 1.5-second cache.
- The browser keeps an eSIM snapshot for 60 seconds and coalesces notification
  workspace loads.
- Network traffic identity uses a 15-second ICCID cache. The device service also
  keeps the SIM snapshot for up to one minute for this lookup.

These caches reduce repeated work, but they do not remove all duplicate work.
The highest-value remaining changes are:

1. Replace raw `RequestURI` logging with a safe route template. The current log
   can record ICCID and other values from a path or query string.
2. Add one combined device status snapshot. A cold status request currently
   reads IMSI and ICCID in both the Identity path and the SIM path.
3. Make background network and SMS polling reuse the same snapshot and
   in-flight work as HTTP requests.
4. Add server-side request coalescing and a short cache for pending eSIM
   notifications.
5. Reduce the one-second SQLite traffic write rate. The current sampler starts
   a transaction and updates the daily row for every sample, including samples
   with no counter change.
6. Correlate each HTTP request with cache outcomes and aggregate AT queue and
   execution time. The current HTTP log contains only total duration.
7. Bound completed operation history. The operation manager retains every
   operation for the life of the process.

## Request Cost Model

One AT manager owns one serialized command session. Parallel HTTP handlers do
not make AT commands execute in parallel. They increase queue depth.

```text
Browser or native client
        |
        v
HTTP handler
        |
        +---- memory snapshot or local configuration
        +---- SQLite
        +---- platform controller or external network
        +---- application cache and request coalescing
                         |
                         v
                  backend adapter
                         |
                         v
                serialized AT queue
                 queue wait + execution
```

API duration is therefore a sum of distinct costs:

```text
request duration
  = HTTP admission and decode
  + application lock or singleflight wait
  + runtime resource wait
  + AT queue wait
  + AT device execution
  + platform, SQLite, or external network time
  + encode and write time
```

A single total-duration field cannot identify which term caused a slow request.

## Existing Cache Inventory

| Owner | Data | Scope | TTL | Coalescing | Invalidation | Assessment |
| --- | --- | --- | --- | --- | --- | --- |
| `application/device` | Identity | Runtime generation | 5 s | Yes | Generation change | Keep. Replace with a combined device snapshot to remove duplicate fields. |
| `application/device` | Radio | Runtime generation | 5 s | Yes | Generation change | Keep. Make the network poller publish through this cache. |
| `application/device` | SIM | Runtime generation | 5 s; 60 s for `CurrentICCID` | Yes | Generation change | Keep. Cache an empty result for a short TTL. |
| `application/esim` | Complete eSIM overview | Runtime generation and local epoch | 10 s | Yes | Profile mutation, generation change | Keep. This is the correct snapshot boundary. |
| `internal/esim` | Overview, chip information, Profile snapshot, discovered AIDs | Manager generation | Event driven | Yes for overview/Profile reads | Reset, card change, validated target failure, mutation | Keep. Add diagnostics for discovery policy and cache outcome. |
| `application/firmware` | Device Control status | Process local | 1.5 s | No | Device Control mutation and settings change | Add generation scope and singleflight. |
| `application/network` | Active ICCID | Process local | 15 s | No | TTL only | Add runtime generation and card-change invalidation. Cache empty results briefly. |
| `application/sms` | Message list | Process local and SQLite | Until refresh | No | Poller, explicit refresh, inbound event | Keep the bounded 500-item view. Coalesce refresh work. |
| Browser eSIM store | Overview and notifications | Browser process | 60 s | Notifications only | Force refresh and mutations | Keep as a presentation cache. Do not treat it as hardware verification. |

All device-derived caches must follow these rules:

- Include runtime generation in the cache key.
- Store only successful results unless a documented negative-cache policy exists.
- Use a bounded TTL.
- Return cloned values when a caller can mutate the value.
- Expose `hit`, `miss`, `coalesced`, and `stale` outcomes to diagnostics.
- Invalidate on reconnect, reset, confirmed card change, and relevant mutation.
- Do not log the cached identifier or payload.

## Endpoint Review

### Device And Runtime

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/device` | Alias of device status. A cold AT read can execute identity, radio, and SIM commands. | 5 s per component, generation scoped | Keep the alias for compatibility. Use one combined backend snapshot. Record cache outcome and AT aggregate time. |
| `GET /api/v1/device/status` | Identity can send `AT+CGSN`, `AT+CIMI`, `AT+QCCID`, `AT+CNUM`, and `AT+QGMR` or `AT+CGMR`. Radio can send registration, operator, network, signal, and serving-cell commands. SIM can send `AT+QSIMSTAT?` or `AT+CPIN?`, then `AT+CIMI` and `AT+QCCID`. | 5 s per component, generation scoped, per-component singleflight | Highest read-path priority. A combined snapshot must reuse IMSI and ICCID within one cold request. Keep EID outside this endpoint. |
| `GET /api/v1/device/capabilities` | Reads the runtime snapshot from memory. | Snapshot is already memory state. | Do not add another cache. Add `ETag` only if payload size becomes material. |
| `POST /api/v1/device/actions/rescan` | Runs the serialized runtime scan and returns the new snapshot. | Not applicable. | Do not cache. Record scan wait, discovery time, backend attach time, and generation change. |
| `POST /api/v1/device/actions/reboot` | Acquires the device resource and sends backend reset, such as `AT+CFUN=1,1`. | Not applicable. | Do not cache. The accepted response is synchronous today. Consider an operation ID if backend reset acceptance can block. |
| `POST /api/v1/device/actions/raw-at` | Executes the user-provided AT command and returns its raw response. | None | Never cache. Record only command class, queue wait, execution time, terminal result, and timeout class. Never record the command or response. |
| `GET /api/v1/runtime/diagnostics` | Builds worker, queue, event, trace, and channel state from memory. | Source state is already in memory. | Do not cache initially. Add bounded API and AT performance summaries to this response. Avoid payloads and identifiers. |
| `GET /api/v1/runtime/traces` | Clones up to 200 bounded message traces. | Bounded memory store. | No extra cache. Add pagination or a limit if trace size grows. |
| `GET /api/v1/runtime/traces/{trace_id}` | Looks up one bounded in-memory trace. | Bounded memory store. | No extra cache. Use a route template in logs. |
| `GET /api/v1/runtime/traces/stream` | Long-lived Server-Sent Events connection with 25 ms coalescing. | Not applicable. | Exclude connection lifetime from normal request-latency SLOs. Record active connections, events sent, bytes, drops, and close reason. |

The WebSocket initial snapshot calls `Device.Status`. A reconnect can therefore
trigger device I/O when the 5-second cache is cold. Keep a last-known device
status snapshot and make the WebSocket handshake device-I/O free. The HTTP
status endpoint remains the explicit refresh path.

### SMS

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `POST /api/v1/sms/actions/refresh` | Reloads SQLite, checks SIM state, lists modem SMS storage, merges messages, then sends `AT+CPMS?` for storage usage. The AT list uses `AT+CMGL=4`. | Bounded message cache, but no refresh singleflight | Do not response-cache an explicit refresh. Coalesce concurrent poller and HTTP refreshes. Return the latest successful snapshot to followers. Avoid reloading all SQLite pages on every 3-second poll. |
| `POST /api/v1/sms/actions/send` | Starts an asynchronous operation. AT uses PDU mode and `AT+CMGS`. | Not applicable. | Keep asynchronous. Record HTTP acceptance latency separately from operation queue, encode, prompt, send, and persistence time. |
| `POST /api/v1/sms/actions/clear` | Deletes modem inbox content, such as `AT+CMGD=1,4`. | Not applicable. | Do not cache. Invalidate or update the message snapshot after success. Consider an operation ID if devices can take several seconds. |

The automatic SMS poller calls `Refresh` every 3 seconds, while the AT backend
also supports `+CMTI` push delivery. Prefer this order:

1. Use `+CMTI` as the primary incremental path.
2. Run a slower reconciliation poll for missed events.
3. Share one refresh flight between the poller and HTTP.
4. Reload SQLite only after a persistence change or process start.

### eSIM

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/esim` | Reads one complete eSIM snapshot. AT uses `AT+CSIM`, `AT+CCHO`, `AT+CGLA`, and `AT+CCHC`. | 10 s generation cache and singleflight; manager cache below it | Keep. This endpoint has the correct snapshot boundary. Report `discovered` or `full_static` policy as a low-cardinality diagnostic field. |
| `GET /api/v1/esim/health` | Composes `Device.Status` and `ESIM.Overview` sequentially. | Reuses both service caches | Do not add an independent health cache. The browser should derive health from already loaded snapshots when possible. The endpoint remains useful for independent clients. |
| `GET /api/v1/esim/notifications` | Reads card notifications through APDU and syncs history to SQLite. A stale target can cause a static AID scan. | Browser-only 60 s cache and request coalescing | Add server-side singleflight and a 5-10 second generation cache. Invalidate after notification actions, Profile actions, reset, and card change. |
| `GET /api/v1/esim/notifications/history` | Reads up to 200 local SQLite history records by default. | SQLite is the source of truth. | Do not add a service cache unless profiling shows a need. Keep the existing result limit. Add pagination or a retention policy if users need older history. |
| `POST /api/v1/esim/notifications/{sequence}/process` | Performs a synchronous card notification action and updates SQLite state. | Not applicable. | Do not cache. Invalidate pending notifications. Use an operation ID if APDU or network retry can exceed the interactive request budget. |
| `DELETE /api/v1/esim/notifications/{sequence}` | Performs a synchronous card notification removal and updates SQLite state. | Not applicable. | Do not cache. Invalidate pending notifications. Record APDU queue and execution aggregates. |
| `POST /api/v1/esim/actions/download` | Starts an asynchronous download and waits for an optional confirmation-code reply inside the operation. | Overview invalidated after success or failure recovery | Keep asynchronous. Record operation phases. Never record activation or confirmation values. |
| `POST /api/v1/esim/actions/enable` | Starts an asynchronous Profile switch under the SIM resource. | Overview invalidated after success | Keep asynchronous. Record VoWiFi teardown, APDU, reset/reconnect, and restore phases separately. |
| `POST /api/v1/esim/actions/disable` | Starts an asynchronous Profile disable under the SIM resource. | Overview invalidated after success | Keep asynchronous. Use the same phase metrics as enable. |
| `POST /api/v1/esim/actions/rename` | Performs a synchronous card mutation. | Overview invalidated after success | Do not cache. Convert to an operation if real-device p95 exceeds the synchronous budget. |
| `POST /api/v1/esim/actions/delete` | Calls `Profiles` before it returns an operation ID, then starts the delete operation. | The manager may satisfy the preflight from cache | Move hardware-dependent bootstrap validation into the operation, or use the application overview snapshot. Operation acceptance must not wait for a cold full scan. |
| `POST /api/v1/esim/operations/{operation_id}/confirmation-code` | Sends an in-memory reply to a waiting operation. | In-memory channel. | No cache. Record only accepted, duplicate, missing, or expired outcome. Never record the code. |

Pending notification cache entries must contain no activation credentials. A
cancelled caller must not cancel a shared load for other callers. Use a detached,
bounded load context or `singleflight.DoChan` with explicit waiter cancellation.

### Network And Traffic

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/network` | AT reads `AT+QCFG="usbnet"?`, then merges device radio and platform interface state. | Device radio has 5 s cache. Network mode and platform status have no service cache. | Add a 2-5 second generation snapshot and singleflight. Invalidate after mode change or reconnect. Keep host counters outside this snapshot if they require 1-second freshness. |
| `POST /api/v1/network/actions/mode` | Starts an asynchronous mode change. AT can send `AT+QCFG="usbnet",...` and reset the modem. | Not applicable. | Keep asynchronous. Invalidate network and device snapshots when the operation starts and after reconnect. |
| `POST /api/v1/network/actions/check` | Runs platform connectivity checks or backend registration checks. | None | Do not cache because this is an explicit probe. Record DNS, route, connect, and backend phases separately. Use an intentional deadline. |
| `GET /api/v1/network/actions/traffic` | Reads host counters or backend traffic values. | None | Prefer the one-second WebSocket traffic event for the UI. A 500-1000 ms cache can coalesce direct API callers. Consider renaming this read path in a future API version. |
| `GET /api/v1/network/traffic/daily` | Resolves ICCID, then reads one SQLite row. | ICCID 15 s; SIM snapshot up to 60 s | Keep the SQLite read uncached. Make ICCID cache generation scoped. Cache an empty ICCID briefly to prevent repeated lookup when no SIM exists. |
| `GET /api/v1/network/traffic/range` | Resolves ICCID, reads up to 30 daily rows, and fills missing dates. | Same ICCID cache | Keep uncached. Add a small response cache only if UI profiling shows repeated identical range reads. Invalidate it after a persisted traffic checkpoint. |
| `GET /api/v1/network/diagnostics` | Runs platform diagnostics and five raw AT queries: USB mode, USB composition, PDP contexts, active contexts, and PDP address. | None | This is an explicit live diagnostic. Do not hide stale data as live. Add `refresh=false` support only if the response clearly reports sample time and age. Record each phase. |

The network status poller calls backend `Radio` and `SIM` directly every 15
seconds. It bypasses the device service cache. Route it through the shared
device snapshot or add a device-service refresh method that updates the cache
and publishes the event.

The traffic sampler runs every second. Before it suppresses an unchanged event,
it calls `RecordTrafficSample`. That function starts a transaction, reads the
previous baseline, writes the daily row, reads the row again, and commits. Use
an in-memory accumulator and persist one checkpoint every 5-15 seconds, on a
meaningful counter change, and during orderly shutdown. Preserve counter-reset
handling and crash-loss limits.

### Calls

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/calls` | Returns the current call and up to 100 history records from memory. | In-memory state from a 3-second poller | No extra cache. This is already a snapshot read. |
| `POST /api/v1/calls/actions/dial` | Sends `ATD<number>;` synchronously. | Not applicable. | Do not cache. Record command class only. Never record the number. Consider operation semantics only if dialing acceptance is slow. |
| `POST /api/v1/calls/actions/reject` | Sends `AT+CHUP` synchronously. | Not applicable. | Do not cache. High-priority command scheduling is appropriate. Record queue wait and execution time. |

The call poller sends `AT+CLCC` every 3 seconds and sends `AT+CLIP=1` once per
service session. This load is intentional for call-state latency. Pause or slow
the poller when the capability is unavailable, the device is not ready, or an
exclusive APDU/device operation is active.

### Device Control

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/device-control` | A cold probe can run EDL detection, `ATI`, `AT+QADBKEY?`, `AT+QCFG="usbcfg"?`, firmware revision, and ADB inspection. | 1.5 s process-local cache | Add runtime generation, singleflight, and cache-outcome diagnostics. Consider a 3-5 second TTL for stable fields and merge volatile session state on every read. |
| `POST /api/v1/device-control/settings` | Persists local settings and invalidates status. | Local settings source | Do not cache the write. Return the stored value. Record local persistence time. |
| `POST /api/v1/device-control/actions/adb-unlock` | Starts an asynchronous exclusive operation. | Not applicable | Keep asynchronous. Record operation phases and tool process duration. |
| `POST /api/v1/device-control/actions/adb-mode` | Starts an asynchronous AT configuration operation. | Status invalidated | Keep asynchronous. Invalidate before and after the operation. |
| `POST /api/v1/device-control/actions/adb/reboot` | Starts an asynchronous ADB process. | Status invalidated | Keep asynchronous. Record process spawn, device selection, and completion. |
| `GET /api/v1/device-control/actions/adb/shell/ws` | Holds an interactive ADB shell and the Device Control lease until close. | Not applicable | Use stream metrics, not request-duration SLOs. Record active sessions, bytes, and close reason without shell content. |
| `POST /api/v1/device-control/actions/usb-id` | Starts an asynchronous USB composition change. | Status invalidated | Keep asynchronous. Record reconnect time separately from command acceptance. |
| `POST /api/v1/device-control/actions/edl` | Starts an asynchronous EDL entry operation. | Status invalidated | Keep asynchronous. Record direct or ADB method as a bounded field. |
| `POST /api/v1/device-control/actions/reset` | Starts an asynchronous Firehose or platform reset. | Status invalidated | Keep asynchronous. Record EDL session correlation and reconnect time. |
| `POST /api/v1/device-control/actions/nand-backup` | Starts a long exclusive backup. | Not applicable | Keep asynchronous. Record bytes, throughput, retries, and phase duration. Do not record user paths. |
| `POST /api/v1/device-control/actions/select-backup-directory` | Opens a native directory selector. | Not applicable | Classify as interactive platform time. Do not include it in normal API latency alerts. |
| `POST /api/v1/device-control/actions/select-edl-directory` | Opens a native directory selector. | Not applicable | Same policy as other selectors. |
| `POST /api/v1/device-control/actions/select-adb-file` | Opens a native file selector. | Not applicable | Same policy as other selectors. |
| `POST /api/v1/device-control/actions/select-loader-file` | Opens a native file selector. | Not applicable | Same policy as other selectors. |

### VoWiFi

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET /api/v1/vowifi` | Reads VoWiFi manager and recovery state from memory. | State is already in memory. | No extra cache. |
| `POST /api/v1/vowifi/actions/enable` | Starts an asynchronous VoWiFi operation. | Not applicable | Keep asynchronous. Record SIM authentication, tunnel, Internet Key Exchange, and IP Multimedia Subsystem registration phases. |
| `POST /api/v1/vowifi/actions/disable` | Starts an asynchronous teardown. | Not applicable | Keep asynchronous. Record teardown and SMS-mode restoration time. |
| `POST /api/v1/vowifi/actions/reconnect` | Starts an asynchronous restart. | Not applicable | Keep asynchronous. Separate teardown and startup duration. |
| `GET, POST, DELETE /api/v1/vowifi/proxies` | Reads or writes local SQLite proxy configuration. | SQLite is the source of truth. | No service cache unless profiling shows repeated reads. Never log passwords or proxy credentials. |
| `GET, POST, DELETE /api/v1/vowifi/proxy-country-rules` | Reads or writes local SQLite rules. | SQLite is the source of truth. | No extra cache. Ensure indexes cover the country-code key. |
| `GET /api/v1/vowifi/country-table` | Reads in-memory table status. | Already in memory. | No extra cache. |
| `GET, PUT /api/v1/vowifi/card-policies` | Reads or writes local card policy. PUT carries ICCID in the query string. | SQLite and in-memory policy state | No extra cache. Move ICCID to a path or body in a future contract. Redact it from current logs now. |

### Notifications, Startup, And Local Configuration

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET, POST /api/v1/notifications/debug` | Reads memory diagnostics or publishes synthetic events. | In-memory state | No extra cache. Keep synthetic payloads bounded. |
| `GET /api/v1/notifications/permissions` | Reads native permission state. | Platform state | No cache unless platform profiling proves it is slow. |
| `POST /api/v1/notifications/permissions/request` | Opens a native permission request. | Not applicable | Interactive platform action. Record accepted outcome, not UI wait time as backend latency. |
| `POST /api/v1/notifications/permissions/open-settings` | Opens native settings. | Not applicable | Interactive platform action. Use the same classification. |
| `GET, PUT /api/v1/notifications/preferences` | Reads or writes local presentation preferences. | In-memory/local configuration | No additional cache. |
| `GET, PUT /api/v1/notifications/channels` | Reads redacted settings or saves and reloads remote channels. | Local configuration and live channel state | No response cache. Record validation, persistence, and reload phases. Never record secrets. |
| `POST /api/v1/notifications/channels/actions/test` | Performs synchronous external delivery. | None | Do not cache. Add an explicit deadline. Record channel type, connect time, and total time. Do not record destination or content. |
| `POST /api/v1/notifications/channels/telegram/chat-ids` | Performs synchronous Telegram discovery. | None | A 10-30 second cache keyed by a non-secret configuration fingerprint is optional. Prefer an operation if it can reach the 120-second client timeout. |
| `GET, PUT /api/v1/settings/startup` | Reads or changes platform login-startup state. | Platform state | No extra cache. Record platform read or write time. |

The OpenAPI document does not currently list the notification channel routes.
It also does not fully describe every multi-method route. Use one route registry
as the source for handler dispatch, OpenAPI paths, route templates, and
performance classification. This prevents documentation and metrics drift.

### SIM Profile Registry, Operations, OpenAPI, And Events

| Endpoint | Current work | Current cache | Decision and optimization |
| --- | --- | --- | --- |
| `GET, POST /api/v1/sim-profiles` | Lists or creates local SQLite records. | SQLite is the source of truth. | No extra cache. Add pagination if the product scope changes from one local device. |
| `PUT, DELETE /api/v1/sim-profiles/{iccid}` | Updates or deletes one local record. The sensitive ICCID is in the path. | Not applicable | Do not cache writes. Use a route template in logs. Never use the concrete path as a metric label. |
| `GET /api/v1/operations/{operation_id}` | Reads one operation from an in-memory map. | In-memory state | No extra cache. Add retention by count and age for terminal operations. Keep active operations until completion. |
| `GET /api/v1/openapi.json` | Rebuilds a map and encodes it for every request. | None | Pre-marshal once at server construction. This is low priority. |
| `GET /api/v1/events/ws` | Sends a device snapshot, then ordered events and keepalive frames. | Device snapshot can use the 5-second cache. | Make initial snapshot device-I/O free. Record active sessions, events, bytes, drops, reconnects, and close reason. Do not record event payloads. |

## Background Work And Queue Contention

The API does not own all device traffic. These workers can occupy the same AT
session while HTTP requests wait:

| Worker | Interval | Device or storage cost | Recommendation |
| --- | --- | --- | --- |
| SMS refresh | 3 s | SIM check, modem SMS list, SQLite reload and merge | Prefer push delivery. Slow reconciliation. Share refresh flight. |
| Call monitor | 3 s | `AT+CLCC`; `AT+CLIP=1` at setup | Keep for call latency. Pause during exclusive work. |
| Traffic sampler | 1 s | Host counters and one SQLite transaction | Keep counter sampling in memory. Persist less often. |
| Network status | 15 s | Direct backend Radio and SIM reads | Route through the device snapshot cache. |
| VoWiFi reconciliation | 30 s and event driven | Can start SIM authentication and network work | Keep event driven. Respect operation barriers and backoff. |
| Runtime scan | Platform-defined interval | Device discovery and possible backend lifecycle work | Keep serialized. Record scan phase duration. |

Priority scheduling alone does not prevent starvation. The AT manager checks the
high-priority queue before normal work. A sustained high-priority producer can
still delay normal status reads. Add queue-depth and oldest-wait diagnostics for
both queues. Set a bounded fairness rule if real traces show starvation.

## Performance Recording Design

### Immediate Privacy Correction

The current completion log uses `r.URL.RequestURI()`. Replace it with a stable
route template, such as `/api/v1/sim-profiles/{iccid}`. Do not log:

- Raw path parameters.
- Query strings.
- IMEI, ICCID, EID, IMSI, MSISDN, phone numbers, or call numbers.
- SMS bodies.
- Activation, confirmation, or matching codes.
- Raw AT commands or responses.
- Proxy, notification, or login credentials.
- User-selected filesystem paths.

The current AT completion record correctly uses a command class instead of raw
command and response data. Keep that policy.

### Per-Request Record

Emit one structured record when a normal request completes:

```json
{
  "event": "http_request_completed",
  "request_id": "process-local-random-id",
  "method": "GET",
  "route": "/api/v1/device/status",
  "status": 200,
  "outcome": "success",
  "duration_ms": 84,
  "response_bytes": 1234,
  "backend": "at",
  "generation": 7,
  "cache_hit_count": 3,
  "cache_miss_count": 0,
  "coalesced_wait_count": 0,
  "at_command_count": 0,
  "at_queue_wait_ms_sum": 0,
  "at_queue_wait_ms_max": 0,
  "at_exec_ms_sum": 0,
  "at_exec_ms_max": 0,
  "sqlite_ms": 0,
  "platform_ms": 0,
  "external_ms": 0
}
```

Use an allowlist for every field. Do not attach response DTOs or error strings.
Record the structured error code and cancellation class instead.

### Correlation

Put a request-scoped performance collector in `context.Context`. The collector
must use bounded counters and durations only. Application, storage, platform,
and modem layers can add spans to it without importing HTTP types.

AT command completion must add its queue and execution values to the active
collector. Background work must use a worker correlation class such as
`sms-poller` or `call-poller`, not a fabricated HTTP request ID.

For asynchronous actions, record two timelines:

- HTTP acceptance: validation through operation ID response.
- Operation execution: pending wait, running phases, and final state.

Do not report a fast `202 Accepted` as proof that the device operation was fast.

### Bounded In-Memory Summaries

Add a bounded performance section to `/api/v1/runtime/diagnostics`:

- Request count by method, route template, status class, and outcome.
- Active request count.
- Duration histograms by route class.
- Cache hit, miss, stale, and coalesced counts by cache name.
- AT command count by command class and terminal result.
- AT normal and high-priority queue depth, capacity, oldest wait, and timeout
  count.
- AT queue-wait and execution histograms.
- SQLite transaction count and duration histogram.
- Active WebSocket and Server-Sent Events connections.
- Async operation queue and run duration by operation type and final state.

Use fixed buckets or a bounded histogram. Do not retain one record per request.
Do not use an identifier, raw path, error message, or operation ID as a metric
label.

### Stream Recording

WebSocket and Server-Sent Events endpoints require connection records:

- Connection opened and closed.
- Active connection gauge.
- Connection duration.
- Messages and bytes sent.
- Ping timeout, write timeout, client close, shutdown, or queue-drop close
  reason.
- Subscription drops and forced resynchronizations.

Do not mix connection duration with ordinary request latency histograms.

## Provisional Performance Budgets

Use these budgets as initial alert thresholds. Replace them with measured
hardware baselines after at least 100 representative samples per route class.

| Class | Provisional target |
| --- | --- |
| In-memory read | p95 below 50 ms |
| Local SQLite read or write | p95 below 100 ms |
| Warm device, network, or eSIM snapshot | p95 below 150 ms |
| Cold combined device status on an idle AT queue | p95 below 5 s |
| eSIM read with a discovered AID | p95 below 8 s |
| Full static eSIM discovery | p95 below 15 s, and it must be explicitly classified |
| AT queue wait during idle UI use | p95 below 500 ms |
| Operation acceptance | p95 below 200 ms unless preflight is intentionally local |
| External connectivity or notification probe | Explicit deadline, separate route class |

A cold full-static eSIM scan is not equivalent to a normal discovered-AID read.
Report the two policies separately.

## Recommended Implementation Order

### Priority 0: Safe And Useful Evidence

1. Introduce a canonical route registry and safe route templates.
2. Remove `RequestURI` and concrete identifier paths from logs.
3. Add request IDs, response byte counts, outcome classes, and route-class
   duration histograms.
4. Correlate AT queue and execution aggregates with HTTP or worker activity.
5. Add HTTP `ReadHeaderTimeout` and `IdleTimeout`. Keep stream compatibility in
   mind before setting a global `WriteTimeout`.

### Priority 1: Remove Repeated Device Work

1. Add a combined device status snapshot port and reuse IMSI and ICCID.
2. Route network polling through the device snapshot service.
3. Coalesce SMS poller and HTTP refresh work. Reduce the reconciliation rate
   when `+CMTI` delivery is healthy.
4. Add server-side pending-notification singleflight and a short cache.
5. Make the WebSocket initial snapshot use last-known memory state only.
6. Add generation scope and singleflight to Device Control status.

### Priority 2: Reduce Storage And Lifecycle Cost

1. Accumulate traffic counters in memory and checkpoint SQLite every 5-15
   seconds and at shutdown.
2. Make the network ICCID cache generation scoped and negative-cache an empty
   result briefly.
3. Bound terminal operation history by age and count.
4. Keep the 200-record notification history result limit. Add pagination or a
   retention policy if users need older history.
5. Pre-marshal the OpenAPI document.

### Priority 3: Improve Explicit Diagnostics

1. Add phase timing to network diagnostics, eSIM operations, Device Control,
   VoWiFi, and external notification probes.
2. Add a `fresh`, `sampled_at`, and `age_ms` contract to responses that can
   serve cached device data.
3. Add a deliberate force-refresh option only where users need live hardware
   verification. Rate-limit it and preserve device arbitration.

## Verification Plan

### Functional Checks

- Confirm that cache hits return the same public schema as cold reads.
- Confirm that runtime generation changes invalidate all device-derived data.
- Confirm that Profile and notification mutations invalidate only relevant
  snapshots.
- Confirm that cancelled followers do not cancel a shared load.
- Confirm that errors are not cached as successful snapshots.
- Confirm that no homepage or ordinary SIM path reads EID.

### Performance Checks

- Run one cold and ten warm calls for each device-read endpoint.
- Run concurrent calls from two clients and confirm one backend load.
- Run the homepage while SMS, call, traffic, and network pollers are active.
- Compare request duration with AT queue-wait and execution sums.
- Force one full-static eSIM discovery and one discovered-AID read. Confirm that
  diagnostics classify them separately.
- Keep traffic counters unchanged for one minute. Confirm that SQLite writes
  follow the checkpoint policy instead of the one-second sample rate.
- Reconnect the event WebSocket with a cold cache. Confirm that the handshake
  sends no AT command.

### Privacy Checks

- Exercise every route that contains ICCID, sequence number, operation ID,
  phone number, activation code, or user path.
- Confirm that logs and diagnostics contain only route templates and bounded
  classes.
- Confirm that AT records contain no command, APDU, response, or identifier.
- Confirm that external notification records contain no destination, token,
  secret, or message content.

### Regression Commands

```sh
go test ./...
go test -race ./internal/application/device ./internal/application/esim ./internal/application/network ./internal/application/sms ./internal/api/http ./internal/modem
npm --prefix web run test
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
```

Use read-only real-device verification unless the user explicitly authorizes a
state-changing command.

## Implemented Policy Record

The `improve-api-performance-observability` change implements these request deadlines:

| Workload class | Deadline |
| --- | --- |
| `memory_read` | 5 seconds |
| `storage_read` | 5 seconds |
| `device_read` | 30 seconds |
| `full_device_read` | 45 seconds |
| `local_command` | 30 seconds |
| `async_accept` | 5 seconds |
| `external_probe` | 45 seconds |
| `stream` | No request deadline |

The implementation uses fixed duration buckets at 5 ms, 25 ms, 100 ms, 500 ms, 1 second, 5 seconds, and 30 seconds. It publishes route summaries and snapshot outcome counters through `/api/v1/runtime/diagnostics`.

The adopted snapshot policies are:

| Snapshot | TTL | Load deadline |
| --- | --- | --- |
| Device Identity, Radio, and SIM | 5 seconds | 30 seconds |
| Device current ICCID | 1 minute | 30 seconds |
| Application eSIM overview | 10 seconds | 45 seconds |
| Pending eSIM notifications | 5 seconds | 45 seconds |
| Device Control stable status | 1.5 seconds | 45 seconds |
| Network active ICCID | 15 seconds | 30 seconds |

Focused in-process tests verify route dispatch, OpenAPI coverage, deadline selection, bounded summaries, privacy, snapshot coalescing, caller cancellation, timeout, cloning, invalidation, error retry, and generation races. These tests do not provide real-device latency measurements.

Real-device measurements are pending. A read-only macOS USB inventory on 2026-08-12 found no supported `2ca3:4006` or `2c7c:0125` device. No device transport was opened and no device command was sent.

Run the measurements only when a supported device is connected. Use read-only routes. Record the active platform, backend, USB identity, runtime generation, sample count, cold duration, warm durations, and snapshot outcomes. Do not record IMEI, ICCID, EID, IMSI, MSISDN, activation data, SMS content, commands, responses, or user paths.
