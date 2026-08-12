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

### Requirement: eSIM reads SHALL use validated discovered targets

The eSIM service SHALL prefer eUICC AIDs discovered in the current device generation, SHALL validate them through a readable card session, and SHALL perform at most one full static discovery fallback when all preferred targets fail. Modem or SIM reset invalidation SHALL prevent stale targets from being reused without validation, and every LPA client SHALL remain scoped to one operation and be closed afterward.

#### Scenario: Known AID remains valid
- **WHEN** a repeated eSIM read starts with a previously discovered AID that opens and returns its EID
- **THEN** the service completes the read without probing unrelated static candidate AIDs and closes the operation's LPA client

#### Scenario: Known AID is stale
- **WHEN** every previously discovered AID fails to open or return a readable EID
- **THEN** the service clears the stale fast path, performs one full static discovery scan, and uses the newly validated target

#### Scenario: Reset invalidates discovery
- **WHEN** the modem or SIM reset boundary is observed
- **THEN** subsequent eSIM reads do not assume the pre-reset target remains valid and rediscover as required

### Requirement: Public eSIM overview SHALL use a lightweight Profile snapshot

The application eSIM overview SHALL obtain only the EID, basic Profile fields required by the stable response, and the live active ICCID tie-breaker. It SHALL NOT require rich eUICC information, configured addresses, certificates, manufacturer lookup, or product-AID fields to return the public Profile overview.

#### Scenario: Client requests Profile overview
- **WHEN** the application handles the public eSIM overview use case
- **THEN** it returns the existing EID and Profile response without issuing rich chip or product-information reads

#### Scenario: Rich details are requested internally
- **WHEN** an internal caller explicitly requests the rich eUICC overview
- **THEN** the manager may perform enrichment without treating a lightweight Profile snapshot as complete rich data

### Requirement: eSIM reads SHALL expose actionable latency evidence

The eSIM manager SHALL log the AID selection policy, fallback occurrence, and aggregate elapsed time for Profile and notification card reads while avoiding unbounded per-APDU success logging.

#### Scenario: Fast Profile read completes
- **WHEN** a Profile snapshot is loaded from a discovered target
- **THEN** structured diagnostics identify the fast-path policy and total read duration

#### Scenario: Discovery fallback occurs
- **WHEN** the preferred target fails and a full scan is attempted
- **THEN** structured diagnostics record that fallback occurred without exposing Profile credentials or activation data

### Requirement: Device-control settings SHALL be owned by the application service

The application service SHALL validate and persist ADB and EDL settings as one atomic device-control document. It SHALL not read tool configuration from browser-only storage or from independent firmware/ADB settings namespaces.

#### Scenario: Settings contain one invalid executable
- **WHEN** a device-control settings request contains an invalid ADB or EDL executable
- **THEN** the service rejects the whole document and leaves the previous document unchanged

#### Scenario: Settings are cleared
- **WHEN** a client submits an explicit empty optional path
- **THEN** the service clears that field in the single device-control document and returns the effective availability reason

### Requirement: Device status SHALL use the shared firmware revision probe

The device-control status path and the device identity path SHALL use the same QGMR-first revision probe and parser. A cached revision SHALL be retained across EDL re-enumeration with an explicit cached source.

#### Scenario: Normal mode status is refreshed
- **WHEN** the modem is in a normal AT-capable mode
- **THEN** status and device identity report the same normalized revision and probe source

#### Scenario: EDL status is refreshed
- **WHEN** the modem is in EDL and no AT channel is available
- **THEN** status retains the last known revision only as cached data and reports that no live probe ran

#### Scenario: Device reconnect is in progress after reset
- **WHEN** the normal USB identity has returned but the runtime backend is still connecting or initializing
- **THEN** device and device-control status return the current reconnecting snapshot without a `device is not ready` API error

### Requirement: Device-control service SHALL arbitrate mode-changing backup work

The device-control service SHALL acquire the existing device resource lock before EDL entry, Firehose read, reset, and reconnect. It SHALL pass context cancellation through every platform and child-process port and SHALL use bounded cleanup deadlines for reset recovery.

#### Scenario: A second device operation starts during EDL backup
- **WHEN** an EDL entry or backup operation holds the device resource
- **THEN** another mode-changing firmware operation receives the existing structured operation-conflict error and no second transport is opened

#### Scenario: Shutdown cancels a backup
- **WHEN** application shutdown closes operation admission while a NAND read is running
- **THEN** the child process and transport are cancelled, the cleanup reset is bounded, and the resource lock is released

### Requirement: Device-control status SHALL report tool and platform capability reasons

The device-control application service SHALL report whether direct EDL switching and complete NAND backup are available, together with a reason for each unavailable method. It SHALL not report backup capability when the configured EDL client cannot perform both read and reset with a validated loader.

#### Scenario: EDL client lacks reset support
- **WHEN** the configured tool can read NAND but cannot run the Firehose reset path
- **THEN** firmware status marks complete backup unavailable and explains that reset support is missing

#### Scenario: Direct switch is unavailable on the active platform
- **WHEN** the platform adapter does not implement the DIAG port
- **THEN** firmware status reports ADB as the available entry method only when an online ADB device can be selected

### Requirement: Reusable snapshots SHALL protect generation and invalidation boundaries

The application snapshot component SHALL scope successful values to a runtime
generation and cache epoch. A late result SHALL NOT populate a different scope.
One shared load SHALL serve concurrent callers for the same scope. A cancelled
caller SHALL stop waiting without cancelling the shared load for other callers.
Errors SHALL NOT become successful cache entries.

#### Scenario: Device reconnects during a load
- **WHEN** the runtime generation changes before a shared load completes
- **THEN** the completed value does not populate the new generation

#### Scenario: First waiter disconnects
- **WHEN** one caller disconnects while another caller waits for the same read
- **THEN** the second caller can receive the bounded shared result

### Requirement: Compatible existing caches SHALL use explicit snapshot policies

Device status components, the application eSIM overview, Device Control stable
probe status, and Network active ICCID memoization SHALL move from local cache
implementations to the reusable snapshot component. Each service SHALL preserve
its existing TTL, cloning, lookup, and invalidation behavior unless this change
specifies a stronger generation rule. The owning service SHALL keep its loader
and invalidation calls explicit. Volatile session state SHALL remain outside
stable cached values.

#### Scenario: Device Control status is warm
- **WHEN** a second status request arrives within the cache TTL and generation
- **THEN** it reuses stable probe data and merges current session state

#### Scenario: Pending notification is removed
- **WHEN** a pending notification removal succeeds
- **THEN** the eSIM service invalidates its pending snapshot before the next list response

#### Scenario: Existing eSIM overview is warm
- **WHEN** the application eSIM overview remains within its ten-second TTL
- **THEN** the service returns a clone from the reusable snapshot without a new card read

#### Scenario: Existing active ICCID value is warm
- **WHEN** Network requests the active ICCID within its current positive TTL
- **THEN** the service uses the reusable snapshot without adding a Network status cache

### Requirement: Migrated services SHALL remove parallel cache implementations

A service that moves an existing cache to the reusable snapshot component SHALL
remove the replaced mutex, timestamp, generation, epoch, and singleflight state.
It SHALL NOT retain a parallel fallback cache for the same read.

#### Scenario: Migration is reviewed
- **WHEN** the implementation of a migrated service is inspected through compiled tests and code review
- **THEN** one snapshot component owns the shared cache mechanics for that read

### Requirement: Snapshot policy SHALL be independent from HTTP policy

An application snapshot load SHALL use its service lifecycle context and its own
finite load deadline. It SHALL NOT use the first HTTP caller context as the
shared load context.

#### Scenario: HTTP request is cancelled
- **WHEN** the first HTTP waiter is cancelled during a shared load
- **THEN** another active waiter can still receive the bounded load result
