## 1. Contracts And Capability Wiring

- [x] 1.1 Add `firmware_edl_switch` and `firmware_nand_backup` capability names and document their reason strings in the domain capability contract.
- [x] 1.2 Add transport interfaces for direct EDL entry, EDL/original-device correlation, and Firehose read/reset operations with context and bounded-time semantics.
- [x] 1.3 Extend runtime/platform wiring so verified platform capabilities are merged into the authoritative device snapshot without overwriting backend capabilities.
- [x] 1.4 Add capability and identity fixtures for normal DJI/Quectel, Qualcomm EDL, missing-location, and ambiguous-device cases; keep all identifiers synthetic.

## 2. Platform USB EDL Switching

- [x] 2.1 Refactor the macOS libusb device enumeration so a requested physical location is matched before an interface is opened or claimed.
- [x] 2.2 Implement allow-listed DIAG interface and bulk endpoint selection for verified `2ca3:4006` and `2c7c:0125` compositions.
- [x] 2.3 Implement the exact `4B 65 01 00 54 0F 7E` write, input drain, seven-byte echo check, timeout, cancellation, and interface release path.
- [x] 2.4 Return an idempotent already-in-EDL result for `05c6:9008` at the target location and reject missing or ambiguous matches.
- [x] 2.5 Add Linux and Windows adapter stubs that return structured unsupported results until a platform implementation passes the same conformance tests.
- [x] 2.6 Add host-safe libusb seam tests for descriptor selection, frame bytes, echo mismatch, timeout, cancellation, release, and location matching.

## 3. Firehose Read And Reset

- [x] 3.1 Define a Firehose runner that executes the configured client without a shell and uses one validated loader and target identity for both `rf` and reset.
- [x] 3.2 Validate the EDL client, loader, output directory, NAND geometry, and temporary output path before starting a read.
- [x] 3.3 Implement the read command with bounded diagnostic capture, an unchanged stdout/stderr operation stream, context cancellation, and geometry/MIBIB/non-empty output validation.
- [x] 3.4 Implement the reset command equivalent to `reset --resetmode=reset` and report acknowledgement or a structured reset failure.
- [x] 3.5 Implement atomic final-file publication and preserve a valid image when reset fails while marking the operation incomplete.
- [x] 3.6 Add runner tests for argument construction, loader reuse, command failure, valid-output/non-zero exit handling, reset failure, cancellation, and bounded logs.
- [x] 3.7 Make the loader override optional for NAND read and Firehose reset, and omit the argument when the EDL client must select its default loader.

## 4. Device Control Application Service

- [x] 4.1 Rename the application service boundary to device control and inject the EDL switcher, correlation port, and Firehose runner while retaining test seams for ADB and EDL command paths.
- [x] 4.2 Update `StartEnterEDL` to accept optional `direct`/`adb` method selection, acquire `ResourceDevice`, record the original identity/location, and wait for matching EDL enumeration.
- [x] 4.3 Keep ADB reboot as an explicit fallback that requires one selected online serial and never selects a different device after direct failure.
- [x] 4.4 Update `StartBackup` to acquire `ResourceDevice`, wait for matching EDL, run read, reset, and reconnect phases, and release the lock on every success, failure, and cancellation path.
- [x] 4.5 Add bounded recovery reset after read failure or cancellation and attach `phase`, `backup_valid`, and `reconnect_required` details to terminal errors.
- [x] 4.6 Extend device-control status to report entry methods, reset availability, complete-backup availability, and server-provided reasons; invalidate the short-lived cache after mode operations.
- [x] 4.7 Add a standalone reset operation that restores normal USB mode and waits for same-location reconnect without reading NAND.

## 5. Device Control Namespace

- [x] 5.1 Rename the application service, status/settings DTOs, REST paths, and operation types from firmware control to the single `device-control` namespace.
- [x] 5.2 Persist ADB command, EDL client path, EDL runner, loader path, and related options in one atomic device-control settings document; remove browser-local EDL settings.
- [x] 5.3 Remove all `/api/v1/firmware` routes, firmware action aliases, and `firmware.*` operation type strings; do not add redirects or delegation handlers.
- [x] 5.4 Add device-control status fields for combined ADB/EDL state, selected method, tool reasons, and firmware revision provenance.
- [x] 5.5 Rename the frontend route, store actions, API client methods, i18n keys, and panel labels to Device Control; remove separate ADB/EDL navigation entries.

## 6. HTTP And Web UI

- [x] 6.1 Add the device-control mode DTO and client service with the optional entry method and new `/api/v1/device-control/actions/*` paths.
- [x] 6.2 Extend the device-control status DTO and OpenAPI declaration with allow-listed entry, backup/reset, and version-source details.
- [x] 6.3 Add HTTP contract tests for new routes, missing legacy routes, direct entry without ADB, explicit ADB fallback, unavailable methods, reset-phase errors, and operation identity stability.
- [x] 6.4 Update the Device Control view and store/types to gate direct EDL, ADB fallback, and backup controls from server data rather than OS or path assumptions.
- [x] 6.5 Render phase-specific progress and the valid-backup-but-reset-failed state, and prevent a second mode action while an operation is active.
- [x] 6.6 Add localized strings for Device Control, method labels, EDL/read/reset/reconnect phases, unsupported reasons, and recovery instructions in every supported locale.
- [x] 6.7 Persist the selected EDL directory immediately, add an optional loader picker, and include the firmware revision in the generated backup filename.
- [x] 6.8 Add a selected-device ADB reboot operation, group normal and EDL reboot commands in one UI selector, and close the shell dialog when the remote shell exits.
- [x] 6.9 Stream complete Firehose stdout/stderr to the operation terminal, use EDL progress directly, hide empty terminals, and treat post-reset reconnect states as valid status snapshots.
- [x] 6.10 Replace the raw-map event allowlist with an evidence-based recursive field blacklist and retain unlisted fields by default.

## 7. Firmware Revision Detection

- [x] 7.1 Add a shared modem revision probe that sends `AT+QGMR` first and `AT+CGMR` only after an error, timeout, or invalid QGMR response.
- [x] 7.2 Implement strict parsing for `+QGMR:`, `+CGMR:`, and unprefixed revision lines, excluding echoes, terminal status, URCs, and ambiguous values.
- [x] 7.3 Return firmware revision source and live/cached freshness through backend identity and device-control status; retain the last normal-mode value across EDL.
- [x] 7.4 Add synthetic AT/backend tests for QGMR success, CGMR fallback, malformed responses, URCs, duplicate values, and EDL cached status.

## 8. Verification And Documentation

- [x] 8.1 Run focused Go tests for domain capabilities, runtime capability merging, platform USB seams, firmware sequencing, device-control routes, and revision parsing.
- [x] 8.2 Run `go test -race ./...` for changed runtime, operation, firmware, and platform lifecycle code, or record the environment blocker.
- [x] 8.3 Run `npm --prefix web run typecheck`, `npm --prefix web run lint`, and `npm --prefix web run build` after UI changes.
- [x] 8.4 Update `docs/code-map/` and firmware/native-bridge documentation with the new device-control ownership and the explicit state-changing EDL/reset safety boundary.
- [x] 8.5 Verify `tools/analyze-nand-diff.py` against synthetic 128 MiB fixtures and document that EFS2/UBI runtime churn is not evidence of firmware corruption.
- [ ] 8.6 After explicit user authorization, perform only read-only hardware verification of DIAG entry, NAND read, Firehose reset, same-location reconnect, and cancellation recovery; do not flash or write partitions.
