## MODIFIED Requirements

### Requirement: Long operations SHALL return an operation identity

SMS sending, eSIM download/switch/disable/delete, network changes, and VoWiFi reconnect operations SHALL return an `operation_id` when completion is asynchronous and SHALL publish progress and terminal status. eSIM download progress SHALL publish the SGP.22 stage (authentication, install, notification) in addition to a coarse percentage, so clients can distinguish a stuck download from an in-flight one.

#### Scenario: eSIM download is accepted
- **WHEN** an eSIM download request passes validation and the device supports the operation
- **THEN** the service returns an `operation_id` and publishes progress followed by a success or structured failure result

#### Scenario: eSIM disable is accepted
- **WHEN** an eSIM disable request targets a profile that is currently enabled and the device supports the operation
- **THEN** the service returns an `operation_id`, publishes progress, and reports the disabled profile state on completion

#### Scenario: eSIM download publishes stage progress
- **WHEN** an eSIM download runs through its SGP.22 stages
- **THEN** the service publishes progress events carrying the current stage (e.g. authenticating with the SM-DP+, installing the bound profile package, sending the install notification) rather than only coarse percentage values

## ADDED Requirements

### Requirement: eSIM download SHALL support interactive confirmation code input

When a download reaches the SM-DP+ authentication step and the activation code or server response requires a confirmation code that the user did not provide upfront, the service SHALL pause the download and request the code through the operation event channel instead of failing immediately. The user's reply SHALL be forwarded as the confirmation code, a negative reply or cancellation SHALL abort the download cleanly with a structured cancellation result, and the request SHALL time out if the user does not reply within a bounded window.

#### Scenario: Download asks for a confirmation code mid-flight
- **WHEN** the SM-DP+ requires a confirmation code and none was supplied with the activation code
- **THEN** the operation pauses and publishes a confirmation-code request event instead of aborting

#### Scenario: User supplies the confirmation code
- **WHEN** the user replies to the confirmation-code request
- **THEN** the download resumes with the supplied code and completes or fails according to the server response

#### Scenario: User cancels during the confirmation-code prompt
- **WHEN** the user declines the confirmation-code request or the request times out
- **THEN** the download is cancelled cleanly and the operation reports a structured cancellation result
