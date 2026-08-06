## MODIFIED Requirements

### Requirement: API errors SHALL use one structured format

Failed API calls SHALL return an error object containing `code`, `message`, `retryable`, and optional `details`; service-layer failures SHALL be classified into explicit structured error codes so validation failures and cancelled operations never fall through to a generic 500; unsupported capabilities SHALL use `capability_not_supported`.

#### Scenario: Unsupported operation is submitted
- **WHEN** a client requests an operation absent from the current capability set
- **THEN** the API returns the structured error with HTTP status appropriate to the failure and identifies the missing capability

#### Scenario: Validation failure in a service
- **WHEN** a service rejects a request for a missing or invalid field, such as an SMS note without an ICCID or with an over-long value
- **THEN** the API maps the validation failure to an explicit structured error code and status instead of returning a generic 500

#### Scenario: Operation was cancelled
- **WHEN** a request targets an operation whose execution was cancelled
- **THEN** the API returns the explicit cancelled-operation structured error instead of a generic 500
