## Purpose

Define the common modem backend contract and truthful capability behavior across AT, QMI, and MBIM implementations.
## Requirements
### Requirement: Modem backends SHALL expose a common business capability contract

AT, QMI, and MBIM backends SHALL expose identity, radio, SIM, SMS, USSD, APDU, capability discovery, event subscription, timeout, and close semantics where supported.

#### Scenario: Backend is selected
- **WHEN** device configuration and interface probing identify a usable AT, QMI, or MBIM implementation
- **THEN** the runtime selects that backend, records the mode and reason, and exposes its capability set

### Requirement: Backend-specific support SHALL be explicit

Each backend SHALL report supported operations as capabilities, and an unsupported operation SHALL return a standard non-success result with code `capability_not_supported` rather than a fabricated result.

#### Scenario: QMI has no raw AT
- **WHEN** a client submits a raw AT command while the selected backend has no `raw_at` capability
- **THEN** the operation is rejected with `capability_not_supported` and identifies `raw_at` in its details

### Requirement: QMI and MBIM startup SHALL NOT require an AT port

The system SHALL initialize QMI or MBIM data and control capabilities when their own control device is available, even when no AT serial port is present.

#### Scenario: MBIM-only device starts
- **WHEN** a device exposes a usable MBIM control device and no AT port
- **THEN** the runtime can initialize MBIM and reports MBIM capabilities without failing on the missing AT port

### Requirement: Modem backends SHALL deliver SMS with true storage identity and decoded content

SMS list, read, and delete operations SHALL use the modem's actual storage indices, SHALL decode PDU content before returning messages, SHALL NOT delete inbound SMS before a registered consumer has durably persisted the complete decoded message, and SHALL serialize storage-selection switching so concurrent inbound notifications cannot interleave with it. Incomplete multipart segments, missing SIM identity, and persistence failures SHALL retain all affected modem entries.

#### Scenario: Client reads an entry after listing
- **WHEN** a client lists SMS and then reads or deletes a specific entry
- **THEN** the backend resolves the entry's real storage index and returns the decoded message or removes the correct message

#### Scenario: Inbound SMS arrives with no consumer
- **WHEN** the modem signals an inbound SMS while no consumer is registered
- **THEN** the backend retains the message in storage instead of deleting it

#### Scenario: Concurrent inbound burst during storage switch
- **WHEN** multiple inbound SMS notifications arrive while the storage selection is being switched
- **THEN** the backend serializes the switch, read, and restore sequence so no message is missed

### Requirement: AT command timeouts SHALL NOT leak responses into subsequent commands

The AT command layer SHALL isolate late responses after a timeout so a subsequent command cannot consume them, SHALL detect URCs before prompt markers, SHALL recognize prompts only as bare prompt text during interactive commands, and SHALL allow the consecutive-timeout watchdog threshold to be configured. If the timed-out command cannot be conclusively terminated within the transport recovery deadline, the manager SHALL abandon or reinitialize the transport before accepting another command.

#### Scenario: Late final response arrives after timeout
- **WHEN** a command times out and a late final response or data line arrives afterwards
- **THEN** the residual line is attributed to the timed-out command, or the transport is quarantined/reinitialized, and the next command observes only its own response

#### Scenario: URC line contains a prompt character
- **WHEN** a URC or USSD line containing a `>` character is received
- **THEN** the line is dispatched as a URC and does not terminate the running command

#### Scenario: Slow but responsive device
- **WHEN** a device is slow but responsive and its responses exceed the default timeout
- **THEN** the modem is not disconnected by the watchdog before the configured threshold is exceeded

### Requirement: APDU coordination SHALL be race-free and exclusive

The APDU arbitration layer SHALL notify waiters of write completion without races or lost wakeups, SHALL notify a lease holder when its lease is force-released, and SHALL NOT grant new exclusive leases or SIM-switch barriers while a force-released lease's APDU is still in flight. If the APDU does not finish before the transport recovery deadline, the arbitration layer SHALL mark the transport quarantined and report it to the transport owner, which SHALL perform recovery/reinitialization; the arbitration layer SHALL NOT close a transport it does not own. No new APDU work SHALL be admitted while the transport is quarantined. If recovery fails, the transport SHALL remain unavailable rather than admitting conflicting work.

#### Scenario: Read waits for a completed write
- **WHEN** a write operation completes and a read request is waiting for it
- **THEN** the reader resumes immediately and never reports a false operation-in-progress

#### Scenario: Long APDU exceeds the lease hold limit
- **WHEN** an in-flight APDU exceeds the lease hold limit and the lease is force-released
- **THEN** the holder is notified, the in-flight APDU is not treated as complete, and no new exclusive lease or SIM-switch barrier is granted until it finishes or the transport recovery path quarantines the transport and its owner reinitializes it

#### Scenario: Force-released APDU never completes
- **WHEN** a force-released APDU remains in flight beyond the transport recovery deadline
- **THEN** the transport is marked quarantined and reported to its owner, no new APDU lease or SIM-switch barrier is granted until recovery completes, and the owner performs the close/reinitialize sequence

### Requirement: Device-controlled protocol data SHALL be parsed within bounded resources

The MBIM and AT parsing layers SHALL validate device-controlled counts and lengths against the remaining buffer before preallocation, SHALL cap fragment and line accumulation, and SHALL clean up incomplete parsing state on cancellation.

#### Scenario: Malicious count in an MBIM payload
- **WHEN** a device-controlled count exceeds what the remaining buffer can contain
- **THEN** the parser rejects the message without allocating memory proportional to the count

#### Scenario: Fragmented MBIM message never completes
- **WHEN** a fragmented MBIM message never completes or its context is cancelled
- **THEN** the fragment collector remains bounded and is eventually removed

#### Scenario: Device emits unending data without a newline
- **WHEN** a device continuously sends data without a newline terminator
- **THEN** line buffering is capped and sustained over-limit input is treated as device misbehavior

### Requirement: Backend event delivery SHALL NOT block command processing

AT and QMI backend event channels SHALL deliver events to subscribers without ever blocking the backend's command loop or event dispatch, SHALL count events dropped for a slow subscriber, and SHALL expose the drop counts so silent loss is diagnosable.

#### Scenario: Slow event consumer on AT
- **WHEN** the AT backend's event channel is full while a new event is published
- **THEN** the send does not block the AT command loop and the dropped event is counted

#### Scenario: QMI event dispatch continues
- **WHEN** the QMI backend's event channel is full
- **THEN** event dispatch continues without stalling and dropped events are counted

### Requirement: SIM identity files SHALL decode consistently across backends

The AT and MBIM paths SHALL decode EF_IMSI to the same full IMSI value: the AT path SHALL NOT truncate the first digit under the parity-bit assumption, and both paths SHALL be pinned by unit tests against the standard 3GPP TS 31.102 layout so the MCC is preserved.

#### Scenario: AT path reads a standard EF_IMSI
- **WHEN** the AT path decodes an EF_IMSI stored in the standard layout
- **THEN** it returns the full IMSI with the MCC intact instead of dropping the first digit

#### Scenario: AT and MBIM paths read the same SIM
- **WHEN** both backends decode EF_IMSI from the same SIM
- **THEN** they return identical IMSI values

### Requirement: Status polling SHALL NOT modify operator format selection

Polling queries SHALL issue only `AT+COPS?` and SHALL parse the format reported by the modem. A polling query SHALL never issue `AT+COPS=3,2`, infer a replacement format, or otherwise modify modem state. An explicit operator-format command is the only path allowed to change the format; a query parse failure SHALL be returned as a classified error.

#### Scenario: Serving-cell polls run repeatedly
- **WHEN** the polling path reads serving-cell or operator state repeatedly
- **THEN** the user's operator format selection is not rewritten by the polling itself

#### Scenario: Format cannot be parsed
- **WHEN** the `AT+COPS?` response does not contain a supported format
- **THEN** polling returns a classified parse error and does not issue any format-setting command

### Requirement: All APDU transports SHALL be coordinated by the device-level arbiter

Every APDU-capable transport, including the pure-AT eSIM port, SHALL share the device-level APDU arbiter instance so SIM-switch barriers and APDU-idle waits apply on all paths and no transport bypasses arbitration.

#### Scenario: Pure-AT eSIM port is used
- **WHEN** the darwin pure-AT eSIM port performs a SIM switch while another component uses the device APDU channel
- **THEN** the switch is coordinated through the same device-level arbiter as the modem path, so the barrier and APDU-idle waits are enforced instead of being no-ops

#### Scenario: VoWiFi AKA auth overlaps an eSIM operation
- **WHEN** VoWiFi AKA authentication and an eSIM APDU operation overlap on the pure-AT path
- **THEN** both are serialized by the shared arbiter and the conflict window is eliminated

### Requirement: AT-channel APDU transport SHALL be implemented once

The near-identical AT APDU channel wrappers SHALL be consolidated into one shared implementation used by all AT-channel APDU consumers, with uniform behavior including rejecting transmissions on channel zero and precompiled command patterns.

#### Scenario: Transmit on channel zero
- **WHEN** an AT APDU channel is asked to transmit on the basic channel (channel zero)
- **THEN** the transmission is rejected uniformly by the shared implementation

#### Scenario: APDU-heavy profile download
- **WHEN** a profile download transmits hundreds of APDUs through the AT channel
- **THEN** command patterns are compiled once at package load, not recompiled per APDU

### Requirement: AT status change handling SHALL distinguish responses from transitions

The AT command layer SHALL retain a status line that belongs to the active query in that command's response and SHALL NOT log, publish, or dispatch it as an unsolicited state change. For genuine unsolicited registration and SIM status reports, the modem manager SHALL compare the parsed value with the last successfully observed value and SHALL perform change handling only when the value is initially unknown or has actually changed.

#### Scenario: Polling receives a stable registration response
- **WHEN** `AT+CEREG?` is active and the modem returns a `+CEREG` response containing the same registration state as the previous poll
- **THEN** the response is returned to the query parser and no registration-change URC is logged or dispatched

#### Scenario: Polling receives a stable SIM response
- **WHEN** `AT+QSIMSTAT?` or `AT+CPIN?` is active and the modem returns its corresponding status response
- **THEN** the response remains part of the command result and does not trigger SIM-change callbacks or modem-reset handling

#### Scenario: Modem repeats an unsolicited state value
- **WHEN** the modem emits a registration or SIM URC whose parsed value matches the last successfully observed value
- **THEN** the manager suppresses change logging, callbacks, and follow-up change handling

#### Scenario: Modem reports a real unsolicited transition
- **WHEN** the modem emits a valid registration or SIM URC whose parsed value differs from the last successfully observed value
- **THEN** the manager updates its baseline and performs the existing state-change logging and dispatch exactly once

### Requirement: Firmware revision probes SHALL use a QGMR-first policy

AT modem backends SHALL query `AT+QGMR` before `AT+CGMR` for the firmware revision. They SHALL use the fallback only when QGMR returns a modem error, timeout, or invalid payload, and SHALL return the command source with the normalized revision.

#### Scenario: DJI/Quectel revision is available through QGMR
- **WHEN** `AT+QGMR` returns a valid single revision line
- **THEN** the backend returns that revision with source `AT+QGMR` and does not send `AT+CGMR`

#### Scenario: QGMR is unsupported
- **WHEN** `AT+QGMR` returns `ERROR` and `AT+CGMR` returns a valid revision
- **THEN** the backend returns the CGMR revision with source `AT+CGMR`

#### Scenario: Both revision commands fail
- **WHEN** both commands return errors or invalid payloads
- **THEN** the backend returns an unknown revision with a non-sensitive reason and does not manufacture a version from another field

### Requirement: Firmware revision parsers SHALL reject ambiguous responses

Revision parsing SHALL remove command echo, terminal status lines, quotes, and unrelated URCs; accept `+QGMR:`/`+CGMR:` and unprefixed revision lines; and reject responses containing zero or multiple plausible revision values.

#### Scenario: Response contains echo and terminal status
- **WHEN** a response contains the command echo, one revision line, and `OK`
- **THEN** the parser returns only the revision line

#### Scenario: Response contains an unrelated URC
- **WHEN** a response contains a registration URC and one revision line
- **THEN** the parser ignores the URC and returns the revision

### Requirement: All AT transports SHALL use one command session

Every AT transport SHALL use the shared command session for command serialization, terminal response classification, unsolicited result code separation, interactive prompts, bounded response collection, timeout quarantine, and transport recovery. A platform-specific transport SHALL NOT implement a separate AT command state machine.

#### Scenario: Linux or Windows serial AT device connects

- **WHEN** a platform adapter supplies an operating-system serial port
- **THEN** the AT factory starts the shared command session on that serial transport

#### Scenario: macOS USB AT device connects

- **WHEN** the macOS adapter supplies an opened USB bulk AT transport
- **THEN** the AT factory starts the same shared command session on that USB transport

#### Scenario: Device returns a terminal error

- **WHEN** any AT transport returns `ERROR`, `+CME ERROR`, or `+CMS ERROR` for the active command
- **THEN** the shared command session returns a command error and does not report the response as success

#### Scenario: Command response arrives after timeout

- **WHEN** an AT command times out on a serial or USB transport
- **THEN** the shared command session isolates the late response before it accepts another command

### Requirement: AT modem behavior SHALL be implemented once

Identity, SIM, radio, Short Message Service, raw AT, network control, Unstructured Supplementary Service Data, SIM authentication, eSIM delegation, and modem event behavior for AT devices SHALL be provided by one AT backend implementation. Platform adapters SHALL NOT duplicate these modem operations.

#### Scenario: The same modem command runs on different platforms

- **WHEN** macOS, Linux, and Windows request the same supported AT modem operation
- **THEN** each platform uses the same backend method, command construction, parser, error behavior, and APDU arbitration rule

#### Scenario: eSIM and SIM authentication share the transport

- **WHEN** eSIM work and SIM authentication use the same AT device
- **THEN** both paths use the same device-level APDU arbiter and the same shared command session

### Requirement: AT command timing SHALL distinguish queueing from execution

The shared AT command session SHALL record bounded structured timing for each
completed command. The record SHALL include queue wait duration, execution
duration, terminal result, and timeout class. It SHALL NOT contain command
payloads, APDU data, responses, activation data, or unmasked identifiers.

#### Scenario: Command waits behind eSIM work

- **WHEN** a status AT command waits in the Manager queue before execution
- **THEN** its diagnostic record reports `queue_wait_ms` separately from `exec_ms`

#### Scenario: Command contains sensitive data

- **WHEN** an AT command contains an APDU, phone number, or credential
- **THEN** the timing record identifies only a safe command class and does not log the command or response content

### Requirement: CommandBackend SHALL provide AT SIM authentication

The raw AT `CommandBackend` SHALL implement `SIMAuthProvider` with AT+CCHO, AT+CGLA, and AT+CCHC. It SHALL validate logical channel bounds and reject malformed APDU responses.

#### Scenario: Open and transmit an AKA channel

- **WHEN** the transport returns `+CCHO: 2` and a valid `+CGLA` response
- **THEN** the backend returns channel 2 and the APDU response payload

#### Scenario: Malformed channel response

- **WHEN** AT+CCHO does not return a positive channel from 1 through 255
- **THEN** the backend returns an error and does not claim APDU success

### Requirement: CommandBackend SHALL report APDU capability

The backend capability set SHALL include `apdu` when its AT transport is present, so the business adapter exposes VoWiFi SIM authentication on macOS USB.

#### Scenario: USB AT transport is present

- **WHEN** a CommandBackend is created with an AT transport
- **THEN** its capability set includes `apdu`

### Requirement: AT SIM authentication SHALL share the device APDU arbiter

When an arbiter is injected, each CommandBackend SIMAuth command SHALL acquire and release a `USIMAKA` transport lease. The macOS adapter SHALL inject the same arbiter instance into the eSIM AT port and CommandBackend.

#### Scenario: VoWiFi and eSIM share APDU arbitration

- **WHEN** the macOS adapter opens a USB AT backend
- **THEN** the CommandBackend and eSIM AT port receive the same device-level arbiter

### Requirement: VoWiFi SHALL accept the direct CommandBackend

The VoWiFi host SHALL obtain the identity, serving-system, operating-mode, and SIM authentication surfaces from a direct CommandBackend without requiring the incompatible legacy `DeviceBackend` SMS contract.

#### Scenario: macOS USB backend starts VoWiFi preparation

- **WHEN** the runtime backend is a CommandBackend that advertises `apdu`
- **THEN** VoWiFi preparation accepts the backend and constructs a logical-channel modem adapter

### Requirement: VoWiFi startup failures SHALL remain visible to the client

The runtime SHALL copy a terminal startup error into the public VoWiFi state. The VoWiFi API SHALL expose the error in `last_error` and the operation status SHALL expose its structured error message.

#### Scenario: SWU tunnel startup fails

- **WHEN** tunnel establishment returns a protocol error
- **THEN** the VoWiFi state becomes `failed`, `/api/v1/vowifi` includes `last_error`, and the operation view displays the error message

