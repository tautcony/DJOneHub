## Purpose

Define application-layer use cases for device features and their operation and domain-data contracts.
## Requirements
### Requirement: Application services SHALL own device feature use cases

Device status, SMS, SIM/eSIM, network, raw AT, and VoWiFi operations SHALL be exposed through application services that depend on domain rules and ports, not directly on HTTP handlers or operating-system commands.

#### Scenario: Service runs without hardware
- **WHEN** an application service is given an in-memory backend and fake transport
- **THEN** it can execute success, timeout, cancellation, and unsupported-capability paths without device nodes or platform commands

### Requirement: Long operations SHALL return an operation identity

SMS sending, eSIM download/switch/delete, network changes, and VoWiFi reconnect operations SHALL return an `operation_id` when completion is asynchronous and SHALL publish progress and terminal status.

#### Scenario: eSIM download is accepted
- **WHEN** an eSIM download request passes validation and the device supports the operation
- **THEN** the service returns an `operation_id` and publishes progress followed by a success or structured failure result

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

