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

