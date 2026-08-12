## Context

The AT backend owns one serialized command session. Device status, network
status, traffic identity lookup, eSIM overview, eSIM health, and eSIM
notifications can enter that session concurrently. The business adapter drops
the optional rich eSIM snapshot surface, ordinary SIM status reads EID, and the
homepage starts several cold device reads together. The result is queueing that
looks like slow hardware even when individual AT commands are healthy.

The runtime still manages one physical device. All caches must be bounded,
generation-scoped, cancellation-aware, and invalidated at reset or reconnect.
Sensitive commands and responses must not be logged.

## Goals / Non-Goals

**Goals:**

- Use one eSIM snapshot read for one overview request.
- Serve repeated identity, radio, and SIM reads from short-lived snapshots.
- Keep EID out of ordinary SIM and homepage status.
- Reuse cached device and eSIM data for eSIM health.
- Reduce the cold-start request burst and coalesce notification reads.
- Preserve validated discovered AIDs until a reset or validated failure.
- Distinguish AT queue delay from device execution time.

**Non-Goals:**

- Parallelize commands on one AT transport.
- Change eSIM write-operation arbitration.
- Remove EID from the eSIM API or eSIM view.
- Treat a cache hit as hardware verification.

## Decisions

### Forward optional eSIM snapshot capability

`BusinessAdapter` will implement `ESIMSnapshotPort` and delegate only when the
wrapped backend implements it. The application eSIM service can then use the
single rich snapshot path instead of composing EID, Profiles, Storage, and
DeviceInfo through separate calls.

### Cache business status by runtime generation

The device service will retain identity, radio, and SIM snapshots for a short
TTL and coalesce concurrent refreshes. The cache key includes runtime
generation. A generation change discards all cached values. Errors are not
cached. Ordinary SIM reads will not call the eSIM port for EID.

### Reuse health inputs

The eSIM health handler will use the cached device status service and the same
cached eSIM overview. It will not request a second forced refresh. The response
continues to expose health fields, but the homepage projection will omit EID.

### Stage browser loading

The homepage will await device status before it starts network, traffic, and
eSIM reads. eSIM notifications will load only when the eSIM notification
workspace needs them. The store will retain one in-flight promise for pending
plus history reads so repeated callers share the same operation.

### Retain discovered AIDs with explicit invalidation

Successful eUICC targets remain generation-scoped. A normal read failure will
validate the target and permit one full static fallback. Reset, reconnect, and
card identity change explicitly clear discovery. Notification reads will not
clear discovery merely because one concurrent request was cancelled.

### Measure queue and execution time without payloads

The Manager will record enqueue time on each request. A structured completion
log will contain operation class, `queue_wait_ms`, `exec_ms`, terminal result,
and timeout. It will not contain raw commands, APDUs, responses, identifiers,
or credentials.

## Risks / Trade-offs

- [Risk] A short status cache can return slightly stale signal data. -> Use a
  small TTL and invalidate on device generation changes and explicit refreshes.
- [Risk] Retaining a stale AID can delay recovery. -> Validate the discovered
  target and allow one bounded full-static fallback.
- [Risk] Staged loading can delay secondary homepage panels. -> Render device
  state first and load independent panels immediately afterward.
- [Risk] Timing logs can become noisy. -> Emit one bounded structured record per
  completed command and keep command content out of the record.

## Migration Plan

1. Add adapter forwarding and status cache tests.
2. Remove EID from ordinary SIM and homepage projections.
3. Reuse eSIM snapshots and adjust AID invalidation.
4. Stage browser requests and coalesce notification reads.
5. Add AT timing diagnostics and focused tests.
6. Run Go, frontend, OpenSpec, race, and macOS build validation.

Rollback removes the new cache and forwarding methods and restores the previous
browser loading order. No persistent data migration is required.

## Open Questions

None.
