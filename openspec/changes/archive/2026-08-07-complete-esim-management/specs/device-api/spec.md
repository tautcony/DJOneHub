## ADDED Requirements

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
