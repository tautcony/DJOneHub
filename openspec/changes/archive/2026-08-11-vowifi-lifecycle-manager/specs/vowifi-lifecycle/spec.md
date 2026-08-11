## ADDED Requirements

### Requirement: VoWiFi SHALL auto-start and reconcile toward the desired state

When VoWiFi is desired (config or user-enabled) and the device is ready, the system SHALL automatically start the session shortly after service startup and SHALL reconcile at a low frequency while the device is connected, so a lost session is pulled back without user intervention. Reconciliation SHALL be single-flight with per-device exponential backoff so flapping events cannot spawn unbounded recovery attempts.

#### Scenario: Session lost while desired
- **WHEN** VoWiFi was desired and the session stops (device reconnect, SIM change, network event) while the device remains ready
- **THEN** the service schedules one recovery at a time with increasing backoff (e.g. 30s/1m/2m) until the session is re-established, and clears the backoff state on success

#### Scenario: Startup auto-start
- **WHEN** the service starts with VoWiFi desired and the device becomes ready within the startup window
- **THEN** the service auto-starts the session once shortly after startup (e.g. 5s) and then keeps a low-frequency reconcile loop

## MODIFIED Requirements

### Requirement: VoWiFi SHALL be owned by the device runtime and application service

VoWiFi enable, disable, reconnect, recovery, SIM/ISIM AKA preparation, ePDG/SWu/IMS state, and tunnel state SHALL be orchestrated through the runtime and service layer rather than directly by API handlers. The implementation SHALL be carried by the `vowifihost.Manager` lifecycle controller plus a host adapter that maps the single-device runtime (backend, modem, SIM AKA provider, operating mode) onto the manager's `Adapter` contract; the HTTP layer only surfaces status and operation requests through the application service.

#### Scenario: VoWiFi is enabled
- **WHEN** a client requests enable and the device, SIM/APDU, network, and packet-tunnel capabilities are ready
- **THEN** the service starts the runtime session, reports progress, and publishes VoWiFi, tunnel, and IMS state

### Requirement: VoWiFi lifecycle transitions SHALL be serialized and converge

Enable, disable, reconnect, and recovery operations SHALL be serialized through one state owner so concurrent transitions cannot interleave on the same port. The `LifecycleController` SHALL serialize commands per device, reject stale generations, and preempt in-flight runs for switch/restart; the runtime store SHALL guard start sessions with epoch claims so duplicate starts cannot both become active. Every failure path SHALL cancel the session context and close any port it opened, the runtime-event subscription SHALL be tied to the session lifecycle, and event-driven recovery SHALL be single-flight and debounced so a flapping network cannot spawn unbounded concurrent recovery attempts.

#### Scenario: Enable fails after the port was opened
- **WHEN** enabling VoWiFi fails after the port was opened
- **THEN** the failure path cancels the session context and closes the port, and a retry starts from a clean state without leaking ports or event consumers

#### Scenario: Network flaps during recovery
- **WHEN** repeated network or modem events trigger recovery within a short window
- **THEN** the host runs one debounced recovery at a time instead of spawning concurrent recovery goroutines
