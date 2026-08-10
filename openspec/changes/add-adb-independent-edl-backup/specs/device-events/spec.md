## MODIFIED Requirements

### Requirement: Event payloads SHALL use typed sanitizers and an evidence-based field blacklist

The public event boundary covers WebSocket events and REST status or snapshot payloads. The boundary SHALL use explicit typed projections for known sensitive payload types. It SHALL use a recursive, case-insensitive field blacklist for raw map payloads. Each blacklist entry SHALL identify a current event or operation producer that can put sensitive data in that field. A raw map field that is not in the blacklist SHALL remain present by default.

The projections SHALL preserve the existing outer shape of `domain.Snapshot` and `device.Status`. Device identity values SHALL remain present in `device.status.changed`, WebSocket snapshot, and REST `device.Status` payloads because the web Overview view masks these values and the loopback boundary restricts access. The sanitizer SHALL replace raw error and reason text with stable fallback text. REST data endpoints, including SMS lists and call history, remain outside this sanitizer.

An `operation.log` event SHALL preserve the exact terminal message. The sanitizer SHALL not remove ANSI sequences, carriage returns, newlines, whitespace-only chunks, or other process-output bytes from that message. The sanitizer SHALL not use content heuristics such as replacing text that contains CJK characters.

#### Scenario: Raw event contains a blacklisted field
- **WHEN** a raw event map contains a field whose current producer can expose an SMS body, phone number, card identifier, hardware serial, local path, raw protocol buffer, or backend error detail
- **THEN** the sanitizer removes that field recursively and preserves non-blacklisted sibling fields

#### Scenario: Raw event contains an unlisted field
- **WHEN** a raw event map contains a new field that has no documented sensitive producer
- **THEN** the sanitizer preserves the field instead of requiring it to be added to an allowlist

#### Scenario: Known SMS or call event is published
- **WHEN** an SMS or call event uses its typed public payload
- **THEN** the typed projection removes the SMS content or call number according to that event contract without adding every typed field name to the raw-map blacklist

#### Scenario: Operation emits terminal output
- **WHEN** a NAND operation publishes stdout or stderr that contains ANSI sequences, carriage returns, newlines, or whitespace-only chunks
- **THEN** the public `operation.log` event preserves the message unchanged for xterm rendering

#### Scenario: Device status is sanitized
- **WHEN** a REST status or WebSocket snapshot payload passes through the typed sanitizer
- **THEN** the `snapshot`, `identity`, `radio`, and `sim` object structure remains present with device identity values intact, while error and reason text uses stable fallback text
