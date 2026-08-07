## ADDED Requirements

### Requirement: Notification listing SHALL target discovered eUICCs

Pending notification listing without an explicit AID SHALL read each distinct eUICC target discovered in the current device generation and SHALL NOT use the complete static compatibility AID table as the notification target list. If no target is available or all preferred targets fail, the service SHALL perform discovery fallback and retain support for devices with multiple distinct eUICCs.

#### Scenario: One eUICC has multiple compatible AIDs
- **WHEN** discovery identifies one EID through a validated primary AID and other static aliases could also open the same card
- **THEN** notification listing reads that eUICC once through the discovered target rather than querying every alias

#### Scenario: eSTK Max exposes two eUICCs
- **WHEN** discovery identifies distinct EIDs for SE0 and SE1
- **THEN** notification listing reads both discovered targets and combines their pending notifications

#### Scenario: Notification target becomes stale
- **WHEN** all discovered notification targets fail to open or return notification data
- **THEN** the service performs one discovery fallback and either returns the recovered notification snapshot or the structured read error
