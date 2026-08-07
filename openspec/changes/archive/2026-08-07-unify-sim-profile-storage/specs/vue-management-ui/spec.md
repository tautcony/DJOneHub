## ADDED Requirements

### Requirement: SIM Profile Management SHALL own all local metadata editing
The management UI SHALL list physical SIMs and installed eSIM Profiles in SIM Profile Management and SHALL create or edit local name, phone, notes, and tags only in that view. The view SHALL distinguish device-observed MSISDN from the user-maintained local phone.

#### Scenario: User edits an eSIM Profile's local metadata
- **WHEN** the user changes local name, phone, notes, or tags for an installed eSIM Profile
- **THEN** SIM Profile Management saves the unified SIM Profile resource and both management and eSIM displays converge on that value

#### Scenario: Device reports a different MSISDN
- **WHEN** a later observation updates the modem-reported MSISDN
- **THEN** the UI retains the user's local phone and presents the observed value as separate device data

### Requirement: The eSIM workbench SHALL edit only the eUICC nickname
The eSIM workbench SHALL display local metadata obtained from the SIM Profile registry but SHALL NOT create or edit it. Its Profile editor SHALL retain only the nickname field that invokes the existing eUICC rename action.

#### Scenario: User opens the eSIM Profile editor
- **WHEN** the user edits an installed eSIM Profile from the eSIM workbench
- **THEN** the editor offers the eUICC nickname and no local name, phone, notes, or tags fields

#### Scenario: Local metadata is unavailable
- **WHEN** the SIM Profile registry request fails after a valid eSIM overview loads
- **THEN** the eSIM workbench keeps Profile operations usable and marks only local metadata as unavailable

## MODIFIED Requirements

### Requirement: eSIM Profile loading SHALL not wait for auxiliary reads

The eSIM route SHALL render a valid Profile overview as soon as the overview request completes. SIM Profile metadata, health, pending notifications, and notification history SHALL be isolated auxiliary states and SHALL NOT delay the route's loaded state or erase a valid Profile snapshot when an auxiliary request fails.

#### Scenario: Overview succeeds while health is slow
- **WHEN** the Profile overview returns before the eSIM health request
- **THEN** the route displays Profiles and leaves health in its independent loading state

#### Scenario: Notification listing is slow
- **WHEN** pending notifications are still loading after Profiles are available
- **THEN** the Profile workspace remains usable and the route is not held in its initial loading state

#### Scenario: Auxiliary request fails
- **WHEN** SIM Profile metadata, health, or notification loading fails after a valid overview is returned
- **THEN** the valid Profile snapshot remains visible and only the affected auxiliary state reports failure or unavailability
