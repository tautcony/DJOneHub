## Purpose

Define application-layer use cases for device features and their operation and domain-data contracts.
## Requirements
### Requirement: Application services SHALL own device feature use cases

Device status, SMS, SIM/eSIM, network, raw AT, and VoWiFi operations SHALL be exposed through application services that depend on domain rules and ports, not directly on HTTP handlers or operating-system commands.

#### Scenario: Service runs without hardware
- **WHEN** an application service is given an in-memory backend and fake transport
- **THEN** it can execute success, timeout, cancellation, and unsupported-capability paths without device nodes or platform commands

### Requirement: Long operations SHALL return an operation identity

SMS sending, eSIM download/switch/disable/delete, network changes, and VoWiFi reconnect operations SHALL return an `operation_id` when completion is asynchronous and SHALL publish progress and terminal status. eSIM download progress SHALL publish the SGP.22 stage (authentication, install, notification) in addition to a coarse percentage, so clients can distinguish a stuck download from an in-flight one.

#### Scenario: eSIM download is accepted
- **WHEN** an eSIM download request passes validation and the device supports the operation
- **THEN** the service returns an `operation_id` and publishes progress followed by a success or structured failure result

#### Scenario: eSIM disable is accepted
- **WHEN** an eSIM disable request targets a profile that is currently enabled and the device supports the operation
- **THEN** the service returns an `operation_id`, publishes progress, and reports the disabled profile state on completion

#### Scenario: eSIM download publishes stage progress
- **WHEN** an eSIM download runs through its SGP.22 stages
- **THEN** the service publishes progress events carrying the current stage (e.g. authenticating with the SM-DP+, installing the bound profile package, sending the install notification) rather than only coarse percentage values

### Requirement: eSIM download SHALL support interactive confirmation code input

When a download reaches the SM-DP+ authentication step and the activation code or server response requires a confirmation code that the user did not provide upfront, the service SHALL pause the download and request the code through the operation event channel instead of failing immediately. The user's reply SHALL be forwarded as the confirmation code, a negative reply or cancellation SHALL abort the download cleanly with a structured cancellation result, and the request SHALL time out if the user does not reply within a bounded window.

#### Scenario: Download asks for a confirmation code mid-flight
- **WHEN** the SM-DP+ requires a confirmation code and none was supplied with the activation code
- **THEN** the operation pauses and publishes a confirmation-code request event instead of aborting

#### Scenario: User supplies the confirmation code
- **WHEN** the user replies to the confirmation-code request
- **THEN** the download resumes with the supplied code and completes or fails according to the server response

#### Scenario: User cancels during the confirmation-code prompt
- **WHEN** the user declines the confirmation-code request or the request times out
- **THEN** the download is cancelled cleanly and the operation reports a structured cancellation result

### Requirement: Services SHALL preserve domain-specific data

The services SHALL expose device identity, SIM/registration/signal/operator state, SMS segmentation and reassembly, eSIM profile state, network diagnostics, and backend capabilities without leaking protocol-specific response formats to callers.

#### Scenario: Long SMS is read
- **WHEN** multipart SMS records are available from a backend
- **THEN** the service reassembles them into one message while retaining enough metadata for verification and cleanup

### Requirement: SMS delivery and reassembly SHALL be consumer-owned and persistent

The SMS service SHALL register as the consumer of inbound SMS notifications, SHALL keep multipart reassembly state across refresh cycles, SHALL keep incomplete segments unacknowledged in modem storage, and SHALL acknowledge all component references only after durable persistence of the complete message; a conflict-ignored insert (identical message already stored) counts as durable persistence. Completed-message event publication SHALL be at-most-once within one running process; end-user notification delivery and process-restart recovery are best-effort. The first refresh after startup SHALL establish a baseline: retained modem entries whose message already exists in storage SHALL NOT be re-published or re-notified as fresh.

#### Scenario: Multipart SMS arrives across polls
- **WHEN** the first segment of a multipart SMS arrives during one refresh cycle and the remaining segments during a later one
- **THEN** the service reassembles them into one message and emits at most one completed delivery within the running process; duplicate segment reads do not emit a second completed message

#### Scenario: Inbound SMS is signaled by the modem
- **WHEN** the modem signals an inbound SMS
- **THEN** the service consumes it through the registered callback, obtains a non-empty SIM identity, leaves incomplete segments in modem storage, and acknowledges all component references only after the complete decoded message is durably persisted (including when the identical message is already stored and the insert is ignored); notification publication is best-effort and reconciled from persisted state

#### Scenario: Process restarts with retained modem entries
- **WHEN** the process restarts while previously persisted messages remain in modem storage (e.g., a crash between persistence and acknowledgement)
- **THEN** the first refresh deduplicates them against stored state without re-publishing or re-notifying, subsequent refreshes publish new deliveries normally, and duplicate reads remain idempotent within the new process

### Requirement: Long-running operations SHALL be cancellable at shutdown

The operation manager SHALL reject new work once shutdown admission is closed, SHALL cancel all in-flight operations, SHALL mark each cancelled operation with the cancelled terminal state, and SHALL release the resource locks the cancelled operations held. Starting work after admission closes SHALL return an explicit shutdown/unavailable error rather than an empty operation identifier. Device read paths SHALL honor context cancellation so a cancelled or disconnected request does not continue occupying device resources.

#### Scenario: Shutdown during a long operation
- **WHEN** the application shuts down while a firmware flash or backup operation is running
- **THEN** the operation is cancelled, reports the cancelled terminal state, and releases its resource locks so shutdown completes

#### Scenario: Operation starts after shutdown admission closes
- **WHEN** an API handler attempts to start an operation after shutdown admission has closed
- **THEN** the operation manager returns the structured shutdown/unavailable error, returns no operation ID, and launches no goroutine

#### Scenario: Client disconnects during an eSIM scan
- **WHEN** a client disconnects while an eSIM overview scan is running
- **THEN** the read path observes the cancelled context and stops promptly instead of completing a full profile scan

### Requirement: SMS storage SHALL deduplicate per SIM identity and list within bounds

The SMS storage layer SHALL scope its deduplication uniqueness key to a non-empty stable SIM identity so an identical message received on a second SIM is stored rather than silently dropped, and SHALL support bounded listing with pagination instead of a full-table scan. If the SIM identity cannot be obtained, the caller SHALL retain the modem entry and retry instead of inserting under a shared empty identity.

#### Scenario: Same SMS arrives on two SIMs
- **WHEN** two SIMs receive the same SMS content within the deduplication window
- **THEN** both messages are stored because the uniqueness key includes the SIM identity

#### Scenario: Existing database upgrades to v3
- **WHEN** a v2 database containing the old table-level SMS uniqueness constraint is opened by the new version
- **THEN** migration v3 transactionally replaces the table and old constraint, preserves existing row IDs and data, and permits otherwise-identical messages with different SIM identities

#### Scenario: SIM identity is temporarily unavailable
- **WHEN** an inbound message is ready to persist but no stable SIM identity can be obtained
- **THEN** the message is not inserted under an empty identity and its modem entry remains available for retry

#### Scenario: Large SMS history is listed internally
- **WHEN** an application service lists SMS from storage with more rows than the bounded page size
- **THEN** storage returns a bounded page for the requested `limit`/`offset`, and the service can iterate pages without requiring a public HTTP pagination parameter

### Requirement: Status polling SHALL avoid redundant publication and probing

Polling services SHALL publish traffic events only when the observed traffic values change, and SHALL serve firmware status from a short-lived cache instead of re-running the full probe sequence (AT queries and ADB probing) on every read. Other status events are unchanged unless separately covered by an explicit event contract.

#### Scenario: Traffic sample is unchanged
- **WHEN** a polling cycle observes the same traffic values as the previous cycle
- **THEN** no traffic event is published for the unchanged sample

#### Scenario: Firmware status is read repeatedly
- **WHEN** firmware status is requested more than once within the cache lifetime
- **THEN** the second and later requests are served from the short-lived cache without repeating the probe sequence

