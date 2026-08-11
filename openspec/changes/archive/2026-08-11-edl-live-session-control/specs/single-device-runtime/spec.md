## ADDED Requirements

### Requirement: Runtime SHALL preserve EDL session identity

The runtime SHALL correlate normal and EDL candidates by stable identity and physical location. It SHALL reject ambiguous or changed-location matches and SHALL enter recovery-required state after bounded observation or reconnect failure.

For an EDL-only cold start, the runtime SHALL establish a session only after the platform adapter finds one unique EDL candidate. After explicit reset, it SHALL match a supported normal USB identity at the same physical location.

#### Scenario: EDL candidate moves location
- **WHEN** an EDL candidate appears at a different physical location
- **THEN** the runtime rejects it and marks the managed session as recovery-required
