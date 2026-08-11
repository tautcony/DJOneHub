## ADDED Requirements

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
