# Application Cache Inventory

## Purpose

This inventory records the cache-shaped state that exists before implementation.
It prevents compatible caches from remaining beside the new snapshot component.
It also prevents unrelated mutable state from entering the snapshot component.

## Decision Rules

Use `migrate` when the state is an immutable successful read snapshot with a TTL
or an explicit invalidation boundary.

Use `retain` when the state is a mutable read model, a protocol work set, event
deduplication state, or a retry throttle.

Use `defer` when a compatible change needs a separate behavior contract.

## Inventory

| Owner and state | Decision | Reason |
| --- | --- | --- |
| `internal/application/device`: Identity snapshot | Implemented | It uses the shared snapshot with a five-second TTL and runtime generation |
| `internal/application/device`: Radio snapshot | Implemented | It uses the shared snapshot with a five-second TTL and runtime generation |
| `internal/application/device`: SIM snapshot | Implemented | It uses the shared snapshot with a five-second TTL and runtime generation |
| `internal/application/esim`: overview snapshot | Implemented | It uses the shared snapshot with a ten-second TTL, runtime generation, and cloning |
| `internal/application/firmware`: Device Control status | Implemented | It uses the shared snapshot with a 1.5-second TTL and merges current EDL session state after each read |
| `internal/application/network`: active ICCID | Implemented | It uses the shared snapshot with a 15-second positive TTL and runtime generation |
| `internal/application/esim`: pending notifications | Implemented | It uses the shared snapshot with a five-second TTL and runtime generation |
| `internal/application/sms`: inbox and sent-message state | Retain | It is a mutable bounded read model that merges storage and modem events |
| `internal/application/sms`: multipart reassembler | Retain | It is a keyed fragment collector with individual expiry |
| `internal/application/extras`: call snapshots | Retain | They are event-driven lifecycle and startup-settling state |
| `internal/application/notification`: seen and delivered sets | Retain | They provide event deduplication and delivery idempotency |
| `internal/esim`: overview, Profile, chip, and discovered-AID state | Retain | The protocol manager patches related values and owns discovery and write recovery |
| `internal/esim`: card-probe failure cooldown | Retain | It controls negative protocol retries for one ICCID |
| `internal/application/firmware`: EDL observations | Retain | They use source-specific reuse windows and mutable session state |
| `internal/application/firmware`: ADB server retry suppression | Retain | It is a process-wide retry throttle |
| `internal/platform`: interface-name caches | Retain | Platform discovery owns and validates these values |
| `internal/backend`: QMI protocol snapshots | Retain | The QMI library owns protocol sampling and validity |
| SMS refresh shared flight | Defer | It needs a no-TTL reconciliation contract and poller coordination |
| Network status snapshot | Defer | It needs a separate freshness and host-interface invalidation contract |

## Completion Check

Implementation is complete only when each `migrate` row uses the shared
snapshot component and its replaced local cache mechanics are removed.

The implementation must update this inventory when it finds another cache-shaped
object in an affected package.
