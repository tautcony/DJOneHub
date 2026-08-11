# device-control Specification

## Purpose
TBD - created by archiving change add-adb-independent-edl-backup. Update Purpose after archive.
## Requirements
### Requirement: Device control SHALL be the single ADB and EDL control surface

The application SHALL expose one device-control service and status model for ADB configuration, EDL configuration, mode entry, USB composition changes, NAND backup, firmware revision, and the related file/directory pickers. The service SHALL persist all ADB and EDL tool settings in one device-control settings namespace.

The service SHALL expose an explicit EDL reset action. The reset action SHALL use the configured Firehose client and optional loader, then wait for the original physical device to reconnect in normal USB mode. The loader setting SHALL be optional; an empty value SHALL let the EDL client perform its default-loader discovery.

#### Scenario: Device-control status is requested
- **WHEN** a client requests the device-control resource
- **THEN** the response contains one status object with ADB, EDL, backup, and firmware-version sections and no separate firmware-control resource

#### Scenario: EDL reset restores normal mode
- **WHEN** a reset operation is requested while the matching device is in Qualcomm EDL
- **THEN** the service runs Firehose reset, waits for the same physical location to return, and reports a retryable phase error if reset or reconnect fails

#### Scenario: Loader is omitted
- **WHEN** a NAND backup or reset request has no loader path
- **THEN** the Firehose command omits the loader override and uses the EDL client's default loader discovery

#### Scenario: Device-control settings are saved
- **WHEN** a client submits valid ADB and EDL executable, runner, loader, and output settings
- **THEN** the service validates and persists them together, or returns one structured validation error without partial writes

### Requirement: Device-control actions SHALL use one operation namespace

All ADB, EDL, USB-ID, NAND backup, and ADB shell actions SHALL use the `device_control.*` operation type prefix and SHALL acquire the existing device resource arbitration. The service SHALL select direct DIAG or ADB entry according to the requested method and advertised capabilities.

#### Scenario: Direct EDL is selected
- **WHEN** the device-control action requests direct EDL and the platform advertises `firmware_edl_switch`
- **THEN** the service starts a `device_control.enter_edl` operation without requiring an ADB serial

#### Scenario: ADB fallback is selected
- **WHEN** the device-control action requests ADB EDL entry
- **THEN** the service requires one online ADB serial and starts the same device-control operation namespace

#### Scenario: ADB normal reboot is selected
- **WHEN** the device-control action requests a normal reboot for one selected online ADB serial
- **THEN** the service acquires the device resource and starts `device_control.adb_reboot` without selecting another ADB device

### Requirement: Device-control namespace SHALL not expose legacy firmware routes

The HTTP server SHALL register only the `/api/v1/device-control` resource and action paths for these controls. It SHALL return not found for the former `/api/v1/firmware` resource and action paths, and it SHALL not register aliases or redirects.

#### Scenario: Legacy firmware path is called
- **WHEN** a client requests `/api/v1/firmware` or `/api/v1/firmware/actions/*`
- **THEN** the server returns not found without invoking a device-control service

### Requirement: Device-control status SHALL expose firmware revision provenance

The status SHALL expose a normalized firmware revision, its probe source, and a non-sensitive reason when no current revision is available. When the device is in EDL, the status SHALL keep the AT firmware revision empty and SHALL expose live EDL facts through the EDL observation.

#### Scenario: QGMR returns a revision
- **WHEN** the modem accepts `AT+QGMR` and returns one valid revision
- **THEN** status reports that revision with source `AT+QGMR`

#### Scenario: Only the fallback command works
- **WHEN** `AT+QGMR` returns an error or invalid payload and `AT+CGMR` returns one valid revision
- **THEN** status reports the revision with source `AT+CGMR`

#### Scenario: EDL has no live AT channel
- **WHEN** the device is detected as Qualcomm EDL and a previous normal-mode revision exists
- **THEN** status keeps the firmware revision empty, reports why AT revision is unavailable, and does not present the cached revision as current EDL data

### Requirement: Device-control status SHALL distinguish EDL facts from AT firmware revision

The status SHALL expose an `edl` observation object with state, protocol, source, and non-sensitive facts. `firmware_revision` SHALL identify only a verified modem revision from AT or another documented source. Cached normal-mode data SHALL never be presented as a live EDL observation.

The `edl` object SHALL contain `state`, optional `protocol`, optional `source`, optional masked `serial_number`, optional masked `hardware_id`, optional masked `pk_hash`, optional `sbl_version`, optional `observed_at`, optional `reason`, and `recovery_required` when recovery is required. `state` SHALL use `detected`, `sahara_connected`, `sahara_identified`, `firehose_ready`, `nand_reading`, `backup_succeeded`, `reset_requested`, `reconnecting`, or `recovery_required`.

#### Scenario: Sahara facts are available
- **WHEN** live Sahara observation returns an SBL version but no verified modem revision
- **THEN** status reports the SBL version under `edl` and keeps `firmware_revision` empty

### Requirement: Backup and reset SHALL have separate semantics

The NAND backup action SHALL not reset or reconnect the device after success. The reset action SHALL be explicit and SHALL report reset and same-location reconnect phases.

A successful backup SHALL finish with progress phase `complete`. It SHALL set the EDL observation state to `backup_succeeded`. It SHALL report that the device remains in EDL. A failed read SHALL use error detail phase `read_nand`, `backup_valid=false`, and `reconnect_required` to report cleanup reset failure. An explicit reset SHALL report progress phases `await_edl`, `reset`, `await_boot`, and `complete`.

#### Scenario: Backup succeeds in EDL
- **WHEN** a valid NAND image is published
- **THEN** backup succeeds without reset and the explicit reset action remains available
