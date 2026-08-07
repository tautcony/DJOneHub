## ADDED Requirements

### Requirement: The API SHALL expose one SIM Profile resource
The HTTP API SHALL expose `GET` and `POST` at `/api/v1/sim-profiles` and `PUT` and `DELETE` at `/api/v1/sim-profiles/{iccid}`. Its stable DTO SHALL expose ICCID, observed IMSI/MSISDN, local name/phone/notes/tags, Profile type, and observation timestamps. State-changing requests SHALL follow the existing local authentication and structured-error contracts.

#### Scenario: Client lists SIM Profiles
- **WHEN** a client requests `GET /api/v1/sim-profiles`
- **THEN** the API returns all registered physical SIM and eSIM Profile records in the unified schema

#### Scenario: Client updates local metadata
- **WHEN** a client puts valid local metadata to `/api/v1/sim-profiles/{iccid}`
- **THEN** the API updates the canonical registry row and returns an explicit structured error when the target does not exist

### Requirement: Obsolete split metadata endpoints SHALL be removed
The API SHALL NOT register `/api/v1/simcards`, `/api/v1/simcards/{iccid}`, or `/api/v1/esim/notes`. OpenAPI SHALL declare only the unified SIM Profile resource for local Profile metadata.

#### Scenario: Client calls a removed endpoint
- **WHEN** a client requests a former SIM card or eSIM-note endpoint
- **THEN** the server returns not found and does not read or mutate Profile metadata

#### Scenario: Client inspects OpenAPI
- **WHEN** a client reads the OpenAPI path declarations
- **THEN** it finds `/api/v1/sim-profiles` and no obsolete local-metadata path
