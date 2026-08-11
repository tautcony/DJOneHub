## Context

The current `EDLPort` only finds USB candidates and `FirehosePort` starts an independent command for each operation. The service locks `ResourceDevice` for the duration of each operation, but it does not retain a protocol session or distinguish observers from the single controller.

## Decisions

1. Add an EDL observation contract below the application service. The platform adapter owns Sahara transport and returns a bounded, sanitized `EDLObservation` with state, protocol facts, and failure reason. Sahara identifiers are masked in public projections.
2. Do not map HWID, PK hash, serial, or SBL version to `firmware`. The normal-mode AT revision remains `firmware_revision`; EDL facts use `edl.*` fields and explicit provenance.
3. Add a runtime-owned `EDLSessionManager` keyed by stable physical location. It stores the matching original identity, current EDL candidate, observation, owner lease, and active operation. It is the only writer for session state.
4. Use a renewable opaque lease token. Read-only status and WebSocket subscription do not need a lease. NAND backup, reset, mode entry, and other device mutations require the current lease and return a structured conflict error for another browser.
5. NAND backup reads and validates the image, publishes the final file, and succeeds without reset. A bounded cleanup reset is allowed only after cancellation or failed protocol setup, and its result is reported separately. Reset remains an explicit operation.
6. Keep asynchronous operation IDs. Session state changes are published as ordered runtime events and are included in the first snapshot.

## State Model

```text
detected -> sahara_connected -> sahara_identified -> firehose_ready
    |              |                  |                    |
 timeout       disconnect         protocol_error       nand_reading
                                                          |
                                                   backup_succeeded (EDL)
                                                          |
                                                   reset_requested
                                                          |
                                                reconnecting / recovered
```

Any transport loss or ambiguous physical match enters `recovery_required` and blocks further mutation until an explicit reset or a fresh session is established.

## Compatibility And Safety

The API changes are additive except for the semantic change that a successful backup no longer implies reconnect. Existing operation details retain `phase`, `backup_valid`, and `reconnect_required`; new details are allow-listed. Session leases are process-local because DJOneHub is a single local service. All deadlines, output buffers, identifiers, and session history remain bounded.

## Verification

Add fake Sahara/EDL tests for every state and failure, service tests proving backup does not call reset, lease concurrency tests for two clients, HTTP contract tests for conflict and snapshot fields, and frontend type/build checks.
