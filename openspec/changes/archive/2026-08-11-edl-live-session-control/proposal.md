## Why

EDL status currently depends on a normal-mode firmware cache. This hides information that the active Sahara device can provide and can associate stale data with the wrong device. NAND backup also performs an implicit reset, which changes device state although backup is a read-only action. Separate browser tabs can start competing device operations because the server has no shared EDL control session.

## What Changes

- Add live Sahara/EDL observation to the device-control status model.
- Keep EDL protocol facts separate from the normal-mode AT firmware revision.
- Model Sahara and Firehose phases, timeout, disconnect, loader, and recovery failures with structured states.
- Make NAND backup finish after a valid image is published and keep the device in EDL.
- Keep normal-mode reset as an explicit operation.
- Add one server-owned EDL session per physical device with a renewable control lease.
- Allow multiple browsers to observe the same session but reject concurrent mutating operations without the lease.
- Publish session and EDL state through the existing snapshot and ordered event streams.

## Capabilities

### New Capabilities

- `edl-session-control`: Live EDL observation and one-device session ownership.

### Modified Capabilities

- `device-control`: Change firmware provenance and backup/reset semantics.
- `firmware-maintenance`: Add Sahara state probing and remove implicit reset after backup.
- `device-api`: Expose EDL observation and session lease contracts.
- `device-events`: Publish EDL session state changes.
- `single-device-runtime`: Preserve one physical-device session across EDL re-enumeration.
- `platform-adapters`: Add verified Sahara observation capability.
- `vue-management-ui`: Render live EDL facts and gate controls by session ownership.

## Impact

Affected areas include `internal/transport`, `internal/platform/darwin`, `internal/application/firmware`, `internal/runtime`, `internal/api/http`, and `web/src/views/FirmwareView.vue`. Existing device-control status and operation schemas gain additive fields. Existing clients that assume backup includes reset must handle the new terminal phase. No new external dependency is required.
