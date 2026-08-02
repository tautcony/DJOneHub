# Cross-Platform Fusion Scope Audit

Date: 2026-08-02

The new DJOneHub surface was checked with `rg` across `cmd/djonehub`,
`internal`, `web/src`, the change artifacts, and delivery documents.

| Surface | Result |
| --- | --- |
| `/api/v1` routes | Single-device device, SMS, eSIM, network, raw AT, operation, and VoWiFi resources only |
| Vue pages | Single-device status, SMS, eSIM, network, raw AT, and VoWiFi workflows only |
| Runtime and domain | One candidate and one backend handle; no device registry or pool |
| Platform adapters | Discovery, transport, network, tunnel, and service ports only |
| `vohive-open/` | Kept as pre-existing reference content; not imported into the DJOneHub product surface |
| Legacy code | Existing macOS eSIM notification handling remains compatibility implementation, not a new notification channel |

No new device pool, proxy orchestration, notification channel, bot, remote
multi-tenant API, or complete VoHive management page was added by this change.
