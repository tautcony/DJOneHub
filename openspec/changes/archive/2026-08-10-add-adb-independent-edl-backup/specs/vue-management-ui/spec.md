## ADDED Requirements

### Requirement: The UI SHALL present one Device Control view

The management UI SHALL replace separate ADB and EDL configuration/control panels with one Device Control view. The view SHALL load and save one settings document and SHALL present method selection, current mode, tool availability, USB composition controls, NAND backup, and firmware revision together.

#### Scenario: Device Control view loads
- **WHEN** the user opens the device-control route
- **THEN** the UI displays one combined status and settings surface without separate ADB or EDL navigation entries

#### Scenario: Device Control settings are edited
- **WHEN** the user changes an ADB or EDL setting and submits the form
- **THEN** the UI sends the complete settings document and reflects the server's effective values and reasons

### Requirement: Firmware version display SHALL show source and freshness

The Device Control view SHALL display the normalized firmware revision, probe source, and whether the value is live or cached. It SHALL render the server reason when no revision is available.

#### Scenario: QGMR version is displayed
- **WHEN** status reports a revision from `AT+QGMR`
- **THEN** the UI displays the revision and identifies the QGMR source

#### Scenario: Version is cached in EDL
- **WHEN** status reports a cached revision while the device is in EDL
- **THEN** the UI labels it as cached and does not present it as a current live probe

### Requirement: Device-control actions SHALL use the server device-control capabilities

The Device Control view SHALL gate direct EDL, ADB fallback, and complete NAND backup controls using the capability data returned by the server. It SHALL display the server-provided reason for an unavailable method and SHALL not infer support from the browser operating system or from an EDL tool path alone.

The EDL panel SHALL provide a separately confirmed restore-normal-mode action. The NAND panel SHALL provide an optional loader picker and SHALL use a default filename that includes the sanitized firmware revision when one is available.

The ADB panel SHALL provide one command selector for normal reboot and reboot-to-EDL. It SHALL apply the selected command only to the selected online ADB device and SHALL request confirmation before either reboot. The ADB mode control SHALL show only the action that changes the current known state. Its label SHALL state that the mode change restarts the device.

#### Scenario: Direct EDL control is available
- **WHEN** firmware status includes `direct` as an available EDL entry method
- **THEN** the view offers direct EDL entry and submits the method explicitly

#### Scenario: Only ADB fallback is available
- **WHEN** firmware status omits direct EDL but reports an online ADB device
- **THEN** the view requires the selected ADB serial and labels the action as the fallback method

#### Scenario: ADB is enabled
- **WHEN** device-control status reports a known enabled ADB composition
- **THEN** the view shows one full-width `Disable ADB and reboot` action and does not show the enable action

#### Scenario: Complete backup is unavailable
- **WHEN** firmware status reports that read/reset capability is unavailable
- **THEN** the view disables the backup action and renders the supplied reason

### Requirement: Device-control operation UI SHALL show recovery phases

The Device Control operation surface SHALL preserve the last operation snapshot while visible and SHALL render phase-specific progress for EDL entry, NAND read, reset, and reconnect. A valid backup with a failed reset SHALL be shown as an incomplete recovery result, not as a successful finished backup.

#### Scenario: Reset phase fails
- **WHEN** an operation completes with `phase=reset`, `backup_valid=true`, and `reconnect_required=true`
- **THEN** the view shows that the image is valid but the device still needs recovery and keeps the terminal error visible

#### Scenario: Device re-enumerates during entry
- **WHEN** the device status changes to offline while an EDL operation is in progress
- **THEN** the view keeps the operation phase visible and does not offer a second entry action until the operation reaches a terminal state

#### Scenario: Operation has no log output
- **WHEN** an ADB configuration or reboot operation reports progress without operation logs
- **THEN** the view shows progress without rendering an empty terminal

#### Scenario: NAND read streams terminal output
- **WHEN** the EDL client emits stdout or stderr during a NAND read
- **THEN** the view renders the complete stream in xterm and uses the EDL-reported percentage as operation progress
