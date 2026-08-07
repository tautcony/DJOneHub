## ADDED Requirements

### Requirement: eSIM Profile loading SHALL not wait for auxiliary reads

The eSIM route SHALL render a valid Profile overview as soon as the overview request completes. Local notes, health, pending notifications, and notification history SHALL be isolated auxiliary states and SHALL NOT delay the route's loaded state or erase a valid Profile snapshot when an auxiliary request fails.

#### Scenario: Overview succeeds while health is slow
- **WHEN** the Profile overview returns before the eSIM health request
- **THEN** the route displays Profiles and leaves health in its independent loading state

#### Scenario: Notification listing is slow
- **WHEN** pending notifications are still loading after Profiles are available
- **THEN** the Profile workspace remains usable and the route is not held in its initial loading state

#### Scenario: Auxiliary request fails
- **WHEN** notes, health, or notification loading fails after a valid overview is returned
- **THEN** the valid Profile snapshot remains visible and only the affected auxiliary state reports failure or unavailability
