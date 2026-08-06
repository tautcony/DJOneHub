## ADDED Requirements

### Requirement: Event payloads SHALL be sanitized by an explicit field allowlist

The event endpoint SHALL sanitize every event and snapshot payload of the public event stream — WebSocket events and REST status/snapshot payloads — through explicit typed projections for known payloads and an explicit field allowlist for raw maps. The projections SHALL preserve the existing outer shape of `domain.Snapshot` and `device.Status`; fields not on the allowlist SHALL be redacted in raw backend events, SMS received bodies, and status payload error/reason text. Device identity (IMEI, ICCID, IMSI, EID) SHALL remain present in `device.status.changed` and `snapshot`/REST `device.Status` payloads, because the web Overview card renders it (client-side masked) and the loopback boundary protects it; only raw error/reason text is replaced with fallback text. REST data endpoints (SMS list, call history) are outside this sanitizer's scope. The sanitizer SHALL NOT rely on content heuristics such as replacing text containing CJK characters.

#### Scenario: Raw backend event contains a disallowed field
- **WHEN** a raw backend event reaches the public event stream carrying fields outside the allowlist
- **THEN** the disallowed fields are redacted while the allowlisted fields are passed through

#### Scenario: SMS body is not allowlisted
- **WHEN** an SMS received event is published and its message body is not on the public allowlist
- **THEN** the body is redacted consistently instead of being passed through and only re-detected by a content heuristic

#### Scenario: Unknown event fields are present
- **WHEN** a call, operation, status, or unknown event contains a field outside its event-family allowlist
- **THEN** the field is redacted in both HTTP and WebSocket output, including when the payload is a raw `map[string]any`

#### Scenario: Device status is sanitized
- **WHEN** a REST status or WebSocket snapshot payload is projected through the allowlist
- **THEN** the `snapshot`, `identity`, `radio`, and `sim` object structure remains present with device identity values intact, while error and reason text is replaced with fallback text
