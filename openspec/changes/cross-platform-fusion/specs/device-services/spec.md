## ADDED Requirements

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
