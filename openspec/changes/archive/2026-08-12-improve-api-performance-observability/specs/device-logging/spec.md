## ADDED Requirements

### Requirement: API completion logs SHALL use route templates

Default API completion and error logs SHALL use the canonical route template,
HTTP method, workload class, status class, structured error code, response size,
and duration. They SHALL NOT include concrete request URIs, query strings,
request bodies, response values, identifiers, credentials, or raw error text.

#### Scenario: Sensitive value appears in a path or query
- **WHEN** an API request contains an ICCID or notification sequence
- **THEN** the completion log contains only the canonical route template
