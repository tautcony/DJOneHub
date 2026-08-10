## Why

The current firmware service splits ADB and EDL configuration, status, and actions across separate names and requires ADB for EDL entry. The reference DJI firmware manager uses a Qualcomm DIAG USB request and detects the modem revision with `AT+QGMR`. DJOneHub currently probes only `AT+CGMR`, so the active firmware version can be blank even when the module reports it through the DJI/Quectel command. The product needs one device-control surface with a complete, repeatable EDL and backup lifecycle.

## What Changes

- Add a single `device-control` service, resource, API namespace, operation namespace, and UI panel for ADB and EDL configuration, status, and actions.
- **BREAKING** Replace the `/api/v1/firmware` resource and `/api/v1/firmware/actions/*` routes with `/api/v1/device-control` and `/api/v1/device-control/actions/*`; do not keep aliases or compatibility delegates.
- Add a platform EDL switch port that can send and verify the Qualcomm DIAG reboot frame on a supported USB identity, including macOS libusb transport.
- Make device-control EDL entry capability-driven. Keep ADB reboot as an explicit fallback only when the platform reports no direct DIAG switch capability.
- Track the device by physical USB location and modem identity across normal-to-EDL re-enumeration. Reject ambiguous matches.
- Add an explicit Firehose reset step after a read-only NAND backup, with bounded wait, cancellation, and a reported failure when reset cannot be confirmed.
- Rename operation types to `device_control.*`, preserve asynchronous operation semantics, and add truthful capability and phase details for entry, backup, reset, and reconnect.
- Detect firmware revision with `AT+QGMR` first and `AT+CGMR` only as a protocol fallback. Return the selected command and parse source.
- Gate device-control actions by the server capability snapshot and show the reason when direct EDL or reset support is unavailable.
- Add protocol-unit, transport-seam, re-enumeration, cancellation, and reset-failure tests. Do not use real device identifiers or NAND contents in fixtures.

## Capabilities

### New Capabilities

- `device-control`: Defines the single ADB/EDL configuration, status, action, and firmware-revision surface.
- `firmware-maintenance`: Defines direct EDL entry, Firehose reset, read-only NAND backup lifecycle, physical-device correlation, and diagnostic phase reporting.

### Modified Capabilities

- `platform-adapters`: Platform adapters must register only verified EDL DIAG switch and Firehose reset capabilities and must preserve physical-location matching across USB re-enumeration.
- `device-services`: Firmware operations must use platform ports, remain cancellable, and reset the module after a successful read-only backup.
- `single-device-runtime`: The authoritative device snapshot must include verified platform firmware capabilities and preserve one-device identity through EDL and boot re-enumeration.
- `modem-backends`: Firmware revision probing must use the DJI/Quectel command first and a standard command as fallback, with strict parsing.
- `device-api`: Device-control status and operation endpoints must expose capability-driven method selection and structured phase/reset/version errors under the new namespace.
- `device-events`: Public raw-map payloads must use an evidence-based recursive field blacklist, while typed projections continue to remove known sensitive fields and operation logs preserve the exact terminal stream.
- `vue-management-ui`: The device-control view must replace the separate ADB and EDL panels and render asynchronous entry, backup, reset, reconnect, and version-probe states truthfully.

## Impact

- `internal/domain/device`: Add stable capability names and device-correlation data needed by firmware operations.
- `internal/platform/darwin` and `internal/platform/linux`: Add or adapt USB DIAG bulk and Firehose reset transports. Unsupported platforms return structured capability errors.
- `internal/application/firmware`: Rename the service boundary to device control, replace the unconditional ADB entry path, sequence backup and reset, and publish bounded operation progress.
- `internal/modem` and `internal/backend`: Add the QGMR-first revision probe and strict response parser.
- `internal/api/http` and `web/src`: Replace the firmware status/action DTOs, routes, operation types, and UI with the single device-control contract.
- `internal/runtime`: Preserve single-device arbitration and reconnect the same physical device after USB mode changes.
- Tests and documentation: Add host-safe fixtures, update the code map and relevant contracts, and document that EDL switching and reset are state-changing operations requiring explicit user authorization.
