## ADDED Requirements

### Requirement: WebSocket upgrades SHALL validate origin and host

The event endpoint SHALL validate the Origin and Host headers during WebSocket upgrade against a loopback origin (any of `http://127.0.0.1:<port>`, `http://localhost:<port>`, or `http://[::1]:<port>` for the bound port) and SHALL reject upgrades that fail either check. Login authentication is deferred and is not part of this contract.

#### Scenario: Malicious page opens the event socket
- **WHEN** a page with a disallowed Origin opens the event WebSocket
- **THEN** the upgrade is rejected and no event stream is established

#### Scenario: Same-origin client reconnects
- **WHEN** a same-origin loopback client reconnects with an allowed Origin and Host
- **THEN** the upgrade succeeds and the session receives a snapshot before incremental events
