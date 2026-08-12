# Route Policy Plan

## Purpose

This plan classifies routes for the typed route registry. It does not assign
application cache behavior to endpoints.

Every route declares:

- HTTP method.
- Canonical path template.
- Workload class.
- Stream kind.
- Handler reference.
- OpenAPI operation metadata.

The registry applies safe completion logging and bounded route metrics to all
routes. It never records concrete paths, query strings, or payloads.

## Workload Assignment

| Workload class | Route groups |
| --- | --- |
| `memory_read` | Capabilities, runtime diagnostics, runtime traces, operation status, OpenAPI, in-memory notification state |
| `storage_read` | SIM Profiles, notification history, traffic history, preferences, startup settings, proxy and policy lists |
| `device_read` | Device status, Device Control status, Network status, call status, VoWiFi status |
| `full_device_read` | eSIM overview, pending eSIM notifications, explicit device diagnostics |
| `local_command` | Synchronous settings, local metadata, picker, permission, and notification commands |
| `async_accept` | SMS send, eSIM Profile operations, mode changes, Device Control operations, and VoWiFi operations |
| `external_probe` | Connectivity checks, notification channel tests, and remote channel discovery |
| `stream` | Event WebSocket, runtime trace stream, and ADB shell WebSocket |

The implementation must review every current route during registry conversion.
The invariant tests must prove that the conversion preserves the complete
method and path set.

## Snapshot Adoption

These application reads adopt the generic snapshot component in this change:

| Application read | TTL | Scope | Invalidation |
| --- | --- | --- | --- |
| Device Identity, Radio, and SIM components | Existing five-second status values and one-minute ICCID value | Runtime generation and epoch | Runtime generation and established service boundaries |
| Application eSIM overview | 10 seconds | Runtime generation and epoch | Existing Profile mutation, reset, and card boundaries |
| Device Control stable probe | 1.5 seconds | Runtime generation and epoch | Control mutation and settings success |
| Network active ICCID | Existing 15-second positive value | Runtime generation and epoch | Runtime generation and card identity boundaries |
| Pending eSIM notifications | 5 seconds | Runtime generation and epoch | Notification action, Profile mutation, reset, card change, or validated target failure |

Volatile EDL session state remains outside the Device Control stable snapshot.
The service merges it for each response.

The first four rows replace existing cache implementations. Pending eSIM
notifications are the only new cache behavior.

## Explicit Exclusions

This change does not add snapshot behavior to:

- SMS refresh.
- Network status. The existing active ICCID memoization still migrates.
- Call status.
- VoWiFi status.
- Notification preferences or channels.
- SIM Profile storage reads.
- Operation status.
- OpenAPI.
- Event streams.
- Mutations and explicit live probes.

These routes still enter the registry. They receive only the common HTTP policy.
