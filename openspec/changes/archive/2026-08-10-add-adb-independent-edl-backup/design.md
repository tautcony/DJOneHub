## Context

DJOneHub already exposes firmware status, an asynchronous EDL action, and an asynchronous NAND backup action. `internal/application/firmware.Service.StartEnterEDL` currently selects an online ADB device and calls `Reboot("edl")`. ADB settings are persisted by the backend, while EDL path and runner settings live in browser local storage. The backup path invokes the configured EDL client with `rf` and accepts a non-empty output file, but it does not send a Firehose reset after the read. The macOS USB transport already uses libusb bulk transfers for AT, but it probes interfaces by sending `AT`; that probe cannot be reused for the Qualcomm DIAG frame.

The extracted reference manager provides two implementation facts. Its Linux helper sends the exact seven-byte frame `4B 65 01 00 54 0F 7E` and requires the same seven-byte echo. Its exit path invokes the Firehose reset command, which sends `<power value="reset"/>`. These facts identify the protocol boundary, but the Linux helper and its raw USBDEVFS calls are not portable to the primary macOS build.

The NAND comparison also provides an operational constraint. The firmware, boot, security, and system partitions are identical between the supplied images. Most changed bytes are in `EFS2` and UBI-backed `usr_data`, where physical page movement and runtime state are expected. A backup workflow must therefore validate transport, geometry, and partition metadata; it must not treat byte-for-byte equality with an older image as a backup success criterion.

## Goals / Non-Goals

**Goals:**

- Enter EDL directly from a supported normal USB composition through a verified DIAG bulk interface, without requiring an ADB server.
- Preserve ADB reboot as a capability-selected fallback for devices and builds that do not provide direct DIAG switching.
- Correlate normal, EDL, and post-reset USB observations to the same physical module and reject ambiguous or mismatched devices.
- Run NAND read and Firehose reset as one cancellable operation lifecycle with bounded waits and truthful terminal results.
- Keep the existing structured error shape and single-device resource arbitration, but publish one new device-control API and operation namespace. The old firmware routes and operation names are removed in the same release.
- Publish enough phase and recovery information for the UI and logs to distinguish entry, read, reset, and reconnect failures.

**Non-Goals:**

- Firmware flashing, partition writes, provisioning, Sahara authentication changes, or any other write operation in EDL.
- A generic Qualcomm USB library for every VID/PID or every EDL protocol variant.
- Making Windows or an unverified Linux/macOS USB interface appear supported.
- Comparing NAND images inside the product or copying real-device identifiers, NAND contents, or activation data into tests or logs.

## Decisions

### 1. Add explicit platform ports for EDL switching and correlation

Add a transport port with operations equivalent to `EnterEDL`, `FindEDL`, and `FindOriginal`, keyed by the current `device.Candidate` and its physical location. Add a separate Firehose runner port with `ReadNAND` and `Reset` operations. The device-control service owns sequencing and operation progress; platform adapters own USB enumeration, interface claiming, endpoint selection, and physical-location matching.

The runtime SHALL merge verified platform firmware capabilities with backend capabilities in the device snapshot. The initial capability names are `firmware_edl_switch` and `firmware_nand_backup`; the latter is advertised only when the read client and a supported loader/reset path are available. A capability reason is required when either capability is absent.

Alternative considered: keep a `DetectEDL func() bool` callback and run all mode changes from the firmware service. Rejected because a boolean cannot identify which physical device is in EDL and would allow a global `05c6:9008` device to be paired with the wrong module.

### 2. Use an allow-listed DIAG interface and an exact frame exchange

For supported DJI and Quectel USB identities, the adapter SHALL enumerate the active USB configuration, select the verified DIAG interface and its bulk IN/OUT endpoints, claim only that interface, drain pending input until a short timeout, and write `4B 65 01 00 54 0F 7E`. The adapter SHALL decode the returned HDLC frame, verify its CRC, and require the decoded payload to equal the exact seven-byte request. If `05c6:9008` is already present at the target location, the operation returns an idempotent `already in EDL` result without writing. AT discovery SHALL use only the allow-listed AT interface and SHALL not probe the DIAG interface with AT commands.

The adapter SHALL never send the reboot frame to every bulk interface or select an interface solely because an `AT` probe failed. Interface numbers and descriptor predicates must be backed by host fixtures and a hardware verification record before a capability is registered. Linux can use libusb or a confined USBDEVFS implementation behind the same port; the reference helper is not invoked from shared application code.

Alternative considered: invoke `qfirehose-edl-switch` as a child process on all platforms. Rejected because the bundled helper is Linux-specific, requires a sysfs path, and cannot run in the macOS application bundle.

### 3. Make entry method selection explicit

`StartEnterEDL` is exposed as `/api/v1/device-control/actions/edl` and runs as `device_control.enter_edl`. The request can carry an optional entry method (`direct` or `adb`); an omitted method selects `direct` when `firmware_edl_switch` is available and otherwise selects `adb`. The ADB serial is required only for the ADB method. A direct attempt must not silently fall back to ADB after a protocol or device error, because that could reboot a different online ADB device.

The operation records the original stable ID and physical location before sending the frame or ADB request. It waits for the expected EDL identity at that location, with a finite deadline. A missing device, a different location, or more than one matching EDL device produces a retryable structured error with the `enter_edl` phase.

### 4. Treat read and reset as one recovery-aware backup transaction

The backup operation uses this sequence:

1. Acquire the existing device resource lock and validate the EDL client, optional loader override, output path, and supported NAND geometry.
2. Wait for a matching `05c6:9008` device at the recorded location.
3. Run `rf` into a temporary output and validate the file size/alignment, MIBIB table, and non-empty content.
4. Run the same Firehose client and loader with `reset --resetmode=reset` (equivalent to `<power value="reset"/>`).
5. Wait for the original USB identity to return at the same location and allow the runtime to reconnect it.
6. Atomically publish the final backup path and report success.

If the read fails or the operation is cancelled after EDL entry, the service still attempts one bounded reset in a cleanup context. A valid backup with a failed reset remains distinguishable from a complete success: the operation fails with structured details such as `phase=reset`, `backup_valid=true`, and `reconnect_required=true`. The service never reports success merely because `rf` returned a non-zero code while a file happens to exist.

The loader override is optional. When no loader is configured, the command omits `--loader` and lets the EDL client use its built-in loader discovery. Device Control also exposes a standalone reset operation so a device that is already in EDL can return to normal USB mode without starting a NAND read.

Alternative considered: close the EDL client after `rf` and rely on USB disconnect or a later user action. Rejected because the reference manager explicitly sends Firehose reset and the current behavior can leave the only device stranded in EDL.

### 5. Keep sensitive diagnostics bounded and useful

Progress messages use stable phase labels (`enter_edl`, `await_edl`, `read_nand`, `reset`, `await_boot`, `complete`). During a NAND read, the operation progress uses the percentage reported by the EDL client. Command output streams to the operation terminal without changing ANSI sequences, carriage returns, newlines, or chunk boundaries. The event subscription and client history remain bounded. Known sensitive event types use typed projections. Raw map payloads use a recursive blacklist that contains only keys with a documented sensitive producer; unlisted fields remain visible by default. Public errors expose phase, retryability, and recovery state, but not raw NAND bytes, loader contents, IMEI, ICCID, or tool stdout/stderr. The operation terminal is not shown when an operation produces no log output. The existing `tools/analyze-nand-diff.py` remains an offline diagnostic tool and is documented as evidence for partition-level comparison, not as a runtime health check.

### 6. Make device control the only control boundary

Rename the application service and public model from firmware management to device control. The canonical resource is `DeviceControlStatus`, and its nested sections cover `adb`, `edl`, `backup`, and `firmware_version`. The canonical settings document contains the executable and tool paths for both ADB and EDL in one backend namespace. The UI has one Device Control view; it does not render separate ADB and EDL configuration panels.

The HTTP surface uses these paths only:

- `GET /api/v1/device-control`
- `POST /api/v1/device-control/settings`
- `POST /api/v1/device-control/actions/adb-unlock`
- `POST /api/v1/device-control/actions/adb-mode`
- `POST /api/v1/device-control/actions/adb/reboot`
- `POST /api/v1/device-control/actions/edl`
- `POST /api/v1/device-control/actions/nand-backup`
- `POST /api/v1/device-control/actions/usb-id`
- `POST /api/v1/device-control/actions/select-backup-directory`
- `POST /api/v1/device-control/actions/select-edl-directory`
- `POST /api/v1/device-control/actions/select-adb-file`
- `GET /api/v1/device-control/actions/adb/shell/ws`

The ADB control panel presents one command selector for the selected online ADB device. The selector provides normal reboot and reboot-to-EDL actions. Both actions use the selected serial and require confirmation. The normal reboot runs as `device_control.adb_reboot`. The EDL reboot uses `device_control.enter_edl` with the explicit `adb` method.

Directory and file pickers use the same `device-control/actions/*` namespace. The former `/api/v1/firmware` routes and `firmware.*` operation types are deleted. No alias, redirect, or dual-write path is introduced. Existing browser-local EDL settings are ignored; the new settings resource starts with explicit empty/default values.

Alternative considered: keep `/firmware` as a delegate to reduce migration risk. Rejected because the requested product model has one control boundary and one version, and duplicate names would continue to split capability and error behavior.

### 7. Use a QGMR-first firmware revision probe

The revision probe sends `AT+QGMR` first because the extracted reference binary contains that command and uses it for the DJI/Quectel modem. If the command returns a modem `ERROR` or an unusable payload, the probe sends `AT+CGMR`. The parser accepts `+QGMR:`/`+CGMR:` responses and unprefixed revision lines, removes echo, quotes, terminal status, and URC lines, and rejects ambiguous multi-value responses.

The status DTO exposes the normalized revision, `firmware_version_source` (`AT+QGMR`, `AT+CGMR`, or backend identity), and a non-sensitive reason when no revision is available. The backend identity path and device-control status path use the same parser and command policy. EDL mode has no AT channel; it retains the last known normal-mode revision and marks its source as cached rather than inventing a version from a loader or raw NAND image.

Alternative considered: use only `AT+CGMR` because it is the standard command. Rejected because the reference tool demonstrates that this module family reports its useful revision through `AT+QGMR`.

## Risks / Trade-offs

- [DIAG interface differs by firmware revision] -> Keep interface selection descriptor-based and allow-listed; register the capability only after a fixture and hardware trace pass.
- [USB re-enumeration is slower than the operation deadline] -> Use separate bounded deadlines for entry, read, reset, and reconnect; return a retryable phase error instead of blocking indefinitely.
- [A reset command fails after a valid read] -> Preserve the valid output, mark the operation failed with `backup_valid`, and expose a manual recovery instruction without claiming completion.
- [Runtime scans race with the temporary EDL absence] -> Hold the device resource lock, record the physical location, and make the discovery/reconnect path identity-aware; do not add a second device manager.
- [Loader/tool paths contain sensitive or unstable local state] -> Validate executable and loader paths before starting, avoid shell evaluation, redact paths from public errors where possible, and keep temporary files in the selected backup directory.
- [Existing callers send only an ADB serial] -> Treat the serial as an optional fallback field and keep the current endpoint and operation ID response shape.

## Migration Plan

1. Add the ports, capability names, device-control status/settings DTOs, and host-only fakes.
2. Implement macOS direct DIAG switching and the Firehose reset sequence. Leave other platforms unsupported until their adapter passes the same conformance tests.
3. Replace the firmware API/UI namespace with the device-control namespace. Existing ADB and EDL settings are not migrated automatically.
4. Run unit, race, and host build checks. Perform only read-only hardware verification with explicit user authorization: DIAG entry and Firehose reset are state-changing recovery actions even though NAND read itself is read-only.
5. Rollback is configuration-safe: disable the new capability provider and use the existing ADB path; do not remove or overwrite existing backup files.

## Open Questions

- Which exact DIAG interface number and endpoint pair is stable for each supported `2ca3:4006` and `2c7c:0125` composition? The implementation must answer this from USB descriptors and a captured, redacted fixture before enabling the capability.
- Should the official loader be bundled, or must the user select a loader whose hash is checked against a maintained allow-list?
- What reconnect deadline is acceptable on the slowest supported module and macOS USB stack?
- Should a failed reset expose a user-facing manual `edl reset` command, or only a retryable operation with a recovery hint?
