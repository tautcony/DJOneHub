## ADDED Requirements

### Requirement: Runtime capability snapshots SHALL include verified platform firmware capabilities

The runtime SHALL compose the backend capability set with the active platform adapter's verified firmware capabilities. It SHALL retain the reason string for every capability and SHALL omit direct EDL or complete-backup capabilities when the platform, tool, loader, or reset path is unverified.

#### Scenario: macOS supports direct EDL and reset
- **WHEN** the active platform adapter verifies the DIAG switch and the configured Firehose client verifies read and reset
- **THEN** the ready-device snapshot includes `firmware_edl_switch` and `firmware_nand_backup` with truthful reasons

#### Scenario: Platform capability is unavailable
- **WHEN** the active adapter has no verified direct DIAG implementation
- **THEN** the snapshot omits `firmware_edl_switch` and exposes the adapter reason through firmware status without a fabricated success capability

### Requirement: Runtime identity SHALL survive EDL and boot re-enumeration

The runtime SHALL keep the original stable identity and physical location as the correlation anchor while the device temporarily disappears for EDL entry or Firehose reset. It SHALL not install a backend for a different location or ambiguous candidate.

#### Scenario: Original device returns after reset
- **WHEN** the original USB identity returns at the recorded physical location after Firehose reset
- **THEN** the runtime reconnects the same managed device and publishes the normal ready snapshot

#### Scenario: A different device returns first
- **WHEN** a candidate with a different physical location or stable identity appears during the reconnect deadline
- **THEN** the runtime rejects it for the current operation and keeps the managed device unavailable until the original target is found or the deadline expires

