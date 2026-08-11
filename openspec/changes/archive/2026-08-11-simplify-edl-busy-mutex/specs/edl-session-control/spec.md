## MODIFIED Requirements

### Requirement: The service SHALL own one EDL session per physical device

The runtime SHALL retain one EDL session for a stable physical location across EDL re-enumeration. A session SHALL have at most one active mutating operation.

The runtime SHALL retain no more than eight inactive physical-location sessions. It SHALL evict the oldest inactive session before it creates a ninth session. It SHALL not evict a session with an active operation.

#### Scenario: Session eviction skips busy sessions
- **WHEN** capacity is full and one session has an active operation
- **THEN** the runtime evicts an idle session, keeping the busy session

#### Scenario: All sessions are busy
- **WHEN** every retained session has an active operation and a new session is requested
- **THEN** the runtime rejects the new session instead of evicting a busy session

### Requirement: The device SHALL be mutually exclusive through a server-side busy state

A device-control action SHALL begin by acquiring the device busy state; the same physical device SHALL NOT run two operations (including an open ADB shell) concurrently. A second concurrent request SHALL receive a structured session-conflict error and no device command SHALL start. The busy state SHALL be released when the operation reaches a terminal state or the shell connection closes. A hung operation SHALL be cancelled after a bounded deadline so the busy state is released consistently with the operation actually ending.

The busy state is server-side and requires no client token, lease, renewal, or WebSocket subprotocol. Any client (any browser tab) may start an operation on an idle device; the events stream keeps other views synchronized.

The device SHALL NOT have a lease endpoint (`/device-control/session/lease`), a lease request header, or a WebSocket lease subprotocol.

#### Scenario: Two concurrent requests reach the device
- **WHEN** a NAND backup or reset is in progress and a second device-control action arrives
- **THEN** the second request receives a structured session-conflict error and no device command starts

#### Scenario: An operation ends
- **WHEN** the operation reaches a terminal state or its shell connection closes
- **THEN** the busy state is released and the next device-control action is accepted

#### Scenario: An idle device accepts any client
- **WHEN** the device is idle and a browser tab starts an operation without any prior token exchange
- **THEN** the operation starts and other tabs learn about it through the events stream

### Requirement: The status read path SHALL not probe during an active operation

While a device-control operation is active, the read-only status path SHALL serve the current session observation without live Sahara probing, so a poll never competes with the Firehose transfer or overwrites operation-recorded state. Live observation SHALL be single-flight: concurrent status polls SHALL NOT open overlapping probes of the same device.

The service SHALL retain verified facts (serial, HWID, PK hash, SBL version) from the last observation when a live probe fails, and SHALL reuse operation-recorded states (`nand_reading`, `backup_succeeded`, `reset_requested`, `reconnecting`) for a bounded interval so a poll does not immediately erase an operation conclusion.

#### Scenario: A status poll arrives during a NAND backup
- **WHEN** the session has an active operation and a browser polls device-control status
- **THEN** the status path serves the current observation without opening a second Sahara session, and the operation state is preserved

#### Scenario: A probe fails after verified facts were observed
- **WHEN** live observation fails transiently
- **THEN** the service reports recovery-required while retaining the previously verified facts for later observation

### Requirement: The websocket snapshot SHALL not probe

The events websocket initial snapshot SHALL be served from the cached device-control status and the current session snapshot. A connection (including reconnects) SHALL NOT trigger AT, ADB, or Sahara probing on the device.

A hung device-control operation SHALL be cancelled after a bounded deadline so the session busy state is released consistently with the operation actually ending; the busy state SHALL NOT be dropped while the operation may still run.

#### Scenario: A client reconnects the events websocket
- **WHEN** the browser reconnects after a network blip while a backup is running
- **THEN** the snapshot is served from cache without USB I/O and the backup is undisturbed
