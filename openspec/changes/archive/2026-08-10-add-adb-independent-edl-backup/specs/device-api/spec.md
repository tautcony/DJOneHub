## ADDED Requirements

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

#### Scenario: Reset fails after a valid image
- **WHEN** Firehose read validation succeeds but reset fails
- **THEN** the operation completion payload contains a retryable reset error with `backup_valid=true` and no raw stderr or NAND data
