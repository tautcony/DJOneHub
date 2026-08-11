## ADDED Requirements

### Requirement: Device Control SHALL render live EDL state and session ownership

The UI SHALL render the server-provided Sahara state, EDL facts, freshness, recovery reason, and lease ownership. It SHALL disable mutating controls when another session owns the device and SHALL not label cached normal-mode data as current EDL firmware.

The UI SHALL store the opaque lease token in `sessionStorage`. It SHALL renew the token before a mutation. It SHALL display masked serial, HWID, PK hash, SBL version, protocol, source, observation time, recovery reason, and active operation when the server provides them.

The UI SHALL display Sahara serial, HWID, PK hash, and SBL fields only when the state is `sahara_identified`. For `detected` or `recovery_required`, it SHALL show the state and a pending or failure reason instead of presenting empty values as facts.

#### Scenario: Another browser controls EDL
- **WHEN** status reports that another client owns the lease
- **THEN** the UI shows the live state and disables NAND backup, reset, and mode mutations

### Requirement: NAND backup SHALL not imply reset

After backup success, the UI SHALL show that the device remains in EDL and SHALL offer reset as a separate confirmed action.

#### Scenario: Backup completes
- **WHEN** the NAND backup operation succeeds
- **THEN** the UI reports a valid backup, keeps EDL mode visible, and does not show reconnect as part of backup
