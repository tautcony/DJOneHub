## ADDED Requirements

### Requirement: Homepage device loading SHALL be staged

The homepage SHALL load device status before it starts secondary network,
traffic, and eSIM reads. Repeated notification refresh calls SHALL share one
pending/history read while it is in flight.

#### Scenario: Homepage opens with a cold AT session

- **WHEN** the management UI loads the homepage for a ready AT device
- **THEN** it completes device status first and does not start all secondary hardware reads in the same initial burst

#### Scenario: Notification refresh overlaps

- **WHEN** more than one caller requests eSIM notifications while a read is active
- **THEN** the callers share the same pending and history request pair

### Requirement: Homepage status SHALL omit EID

The homepage device-status presentation SHALL NOT display EID. The eSIM view
SHALL continue to display EID from the eSIM response.

#### Scenario: User views the homepage

- **WHEN** a connected eSIM device appears on the homepage
- **THEN** the homepage omits EID and shows the remaining device, SIM, and network status

#### Scenario: User opens the eSIM view

- **WHEN** the eSIM overview loads successfully
- **THEN** the eSIM view displays the EID from the eSIM snapshot
