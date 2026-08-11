## Purpose

Define the versioned local HTTP and WebSocket-facing API for managing one device and its supported capabilities.
## Requirements
### Requirement: The API SHALL provide versioned single-device resources

The HTTP API SHALL expose `/api/v1` resources for device identity, capabilities, status, rescan, raw AT, SMS, eSIM, network, and VoWiFi, plus the `/api/v1/events/ws` event endpoint.

#### Scenario: Client reads device state
- **WHEN** a client requests `GET /api/v1/device/status`
- **THEN** the API returns the current single-device state, backend, capability-derived status, and a stable response schema

#### Scenario: Client requests rescan
- **WHEN** a client posts to `/api/v1/device/actions/rescan`
- **THEN** the API validates the request, invokes the application service, and returns the operation or result without opening a transport in the handler

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

### Requirement: API handlers SHALL enforce the local authentication boundary

Until the first-use authentication flow is specified, the process SHALL reject any listen address whose host is not a loopback form (`127.0.0.1`, `localhost`, or `::1`). State-changing API handlers SHALL validate Origin/Host and SHALL reject cross-site simple writes before invoking application services. No credential check is required by this temporary boundary; login authentication remains deferred.

#### Scenario: Non-loopback server is requested
- **WHEN** the server is configured with a wildcard, non-loopback, or hostname listen address
- **THEN** startup fails before the application or native UI begins serving

#### Scenario: Loopback server is requested
- **WHEN** the server is configured with `127.0.0.1`, `localhost`, or `[::1]`
- **THEN** startup proceeds and the temporary loopback boundary applies

#### Scenario: Cross-site simple request reaches a write endpoint
- **WHEN** a cross-site request submits a write operation with a non-JSON content type or a disallowed origin
- **THEN** the API rejects the request before invoking the application service

#### Scenario: Same-origin write is submitted
- **WHEN** a client submits a state-changing request from an allowed loopback origin
- **THEN** the API accepts it subject to normal method, content-type, and DTO validation

#### Scenario: Unauthenticated command is submitted
- **WHEN** a same-origin loopback client submits a protected device command without login credentials
- **THEN** the temporary boundary does not reject it for missing credentials and still applies Origin/Host, method, content-type, DTO, and capability validation

### Requirement: OpenAPI declarations SHALL match the temporary API boundary

The OpenAPI document SHALL not declare login credentials or a security scheme that the server does not enforce.

#### Scenario: Security scheme is inspected
- **WHEN** a client reads the OpenAPI document
- **THEN** it contains no deferred credential requirement

### Requirement: The API SHALL expose eSIM disable and notification operations

The HTTP API SHALL expose an eSIM disable action and an eUICC notification resource. `POST /api/v1/esim/actions/disable` SHALL accept an `iccid` and return an `operation_id` like the existing enable action. Notification endpoints SHALL operate on the pending eUICC notification list: `GET /api/v1/esim/notifications` SHALL list pending notifications with sequence number, event type, and profile identifier; `POST /api/v1/esim/notifications/{seq}/process` SHALL retry sending the notification and remove it on success; `DELETE /api/v1/esim/notifications/{seq}` SHALL remove the notification without sending. Unsupported capabilities SHALL keep returning `capability_not_supported`.

#### Scenario: Client disables a profile
- **WHEN** a client posts an `iccid` of an enabled profile to the disable action
- **THEN** the API validates the request, invokes the application service, and returns an `operation_id` for async progress

#### Scenario: Client lists pending notifications
- **WHEN** a client requests the eUICC notification list
- **THEN** the API returns the pending notifications in the stable response schema, or an empty list when none are pending

#### Scenario: Client retries a notification
- **WHEN** a client posts to the process action for an existing sequence number
- **THEN** the API invokes the service and returns the retry result, with a structured error when the retry fails and the notification stays pending

#### Scenario: Client removes a notification
- **WHEN** a client deletes an existing sequence number
- **THEN** the API invokes the service and confirms the removal, with a structured error for an unknown sequence number

#### Scenario: Notification endpoints are unsupported
- **WHEN** a client targets the notification endpoints while the capability snapshot does not include eSIM notifications
- **THEN** the API returns `capability_not_supported` like other unavailable eSIM operations

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

### Requirement: Device-control APIs SHALL use one public namespace

The HTTP API SHALL expose device-control status, settings, ADB, EDL, NAND backup, USB-ID, picker, and shell operations below `/api/v1/device-control`. The former `/api/v1/firmware` paths SHALL be removed. Operation responses SHALL keep the asynchronous `operation_id` envelope while using `device_control.*` operation types.

#### Scenario: Device-control status is inspected
- **WHEN** a client requests `GET /api/v1/device-control`
- **THEN** it receives the complete ADB/EDL/control/version status and no `/firmware` resource is required

#### Scenario: Legacy route is inspected
- **WHEN** a client requests a former `/api/v1/firmware` path
- **THEN** the server returns not found and does not delegate to device control

### Requirement: Device-control mode and backup APIs SHALL expose capability-driven method details

The device-control mode and backup endpoints SHALL use the new device-control paths and retain the `operation_id` response shape. The EDL mode request SHALL accept an optional `method` value (`direct` or `adb`) and an optional serial used by the ADB method. Device-control status SHALL expose the available entry methods and complete-backup/reset availability with structured reasons. Unsupported or invalid method selections SHALL use the existing structured error contract.

The API SHALL expose `POST /api/v1/device-control/actions/reset` for standalone EDL recovery and `POST /api/v1/device-control/actions/select-loader-file` for the optional NAND-stage loader picker. The EDL directory picker SHALL be followed by persistence of the complete settings document by the UI.

The API SHALL expose `POST /api/v1/device-control/actions/adb/reboot` for a normal reboot of one selected online ADB device. The request SHALL require the selected ADB serial and SHALL return an asynchronous `operation_id` response.

#### Scenario: Client requests direct EDL without ADB
- **WHEN** a same-origin client posts `{"mode":"edl","method":"direct"}` and the direct capability is present
- **THEN** the API accepts the request and returns an operation ID without requiring `serial`

#### Scenario: Client selects ADB fallback
- **WHEN** a client posts `{"mode":"edl","method":"adb","serial":"device-1"}`
- **THEN** the API validates the serial, invokes ADB reboot, and returns the existing asynchronous response shape

#### Scenario: Client requests a normal ADB reboot
- **WHEN** a client posts one selected online ADB serial to `/api/v1/device-control/actions/adb/reboot`
- **THEN** the API starts `device_control.adb_reboot` for that device and returns an operation ID

#### Scenario: Client selects an unavailable method
- **WHEN** a client selects `direct` without the direct capability or selects `adb` without an online serial
- **THEN** the API returns `capability_not_supported` or `device_offline` with phase and method details before starting an operation

### Requirement: Device-control API errors SHALL identify recovery state

Terminal firmware operation errors SHALL preserve `code`, `message`, and `retryable`. Phase-aware failures SHALL restrict details to the allow-listed fields `phase`, `method`, `backup_valid`, and `reconnect_required`. Raw tool output, NAND content, and unmasked device identifiers SHALL not be returned.

#### Scenario: Explicit reset fails
- **WHEN** the standalone EDL reset or normal-mode reconnect fails
- **THEN** the operation completion payload contains a retryable phase error with `reconnect_required=true` and no raw stderr or NAND data

### Requirement: Device-control API SHALL expose EDL session state and lease operations

The device-control status response SHALL include the current EDL observation and session ownership state. The API SHALL provide bounded lease acquire, renew, and release actions. Mutating actions SHALL require the current lease and SHALL return `device_session_conflict` when another client owns it.

The API SHALL use `/api/v1/device-control/session/lease` for lease actions. `POST` SHALL acquire a lease and return HTTP 201 with `lease_token` and `session`. `PUT` SHALL renew a lease. `DELETE` SHALL release a lease. `PUT`, `DELETE`, and each device-control mutation SHALL read the opaque token from `X-DJOneHub-Device-Lease`.

The browser WebSocket API cannot set a custom header. The ADB Shell WebSocket SHALL carry the same token in the `djonehub-device-lease.<token>` WebSocket subprotocol. The server SHALL validate the token before it opens the device shell. The server SHALL pin the lease until the WebSocket closes.

The `session` object SHALL contain `session_id`, `observation`, `lease_held`, `lease_owned`, optional `lease_expires_at`, and optional `active_operation`. The public API SHALL omit `physical_location`. The public API SHALL mask `serial_number`, `hardware_id`, and `pk_hash`.

The `device_session_conflict` error SHALL use HTTP 409. Its public details MAY contain `session_id`. Its public details SHALL NOT contain the lease token or physical location.

#### Scenario: Another client owns the lease
- **WHEN** a client submits a mutating action with no current lease token
- **THEN** the API returns `device_session_conflict` and starts no operation

#### Scenario: The owner reads status
- **WHEN** a client supplies the current token on `GET /api/v1/device-control`
- **THEN** `edl_session.lease_owned` is true for that response only
