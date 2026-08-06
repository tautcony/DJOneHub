## Purpose

Define the build and repository hygiene contract for the root product so the binary is assembled from a lean, verified module graph without legacy configuration surfaces, dead subsystems, or unsafe build-time behavior.

## Requirements

### Requirement: Legacy configuration surfaces SHALL be removed

The application config module SHALL NOT retain the legacy viper-backed load surface (`Load`, `GetConfig`, `UpdateNotificationInFile`, and the Telegram, Feishu, QQ, Webhook, Bark, Email, and Pushplus notification-channel configuration types), SHALL NOT expose shared mutable pointers to internal state, and SHALL NOT compile the viper dependency into the binary.

#### Scenario: Binary builds without viper
- **WHEN** the root module is built after the legacy config surface is removed
- **THEN** `go build ./...` succeeds without the viper dependency and no legacy config function remains referenced

#### Scenario: Config access never returns shared mutable state
- **WHEN** application code reads device or configuration data through the config module
- **THEN** it receives copies or read-only views rather than pointers into shared mutable state

### Requirement: The state and config directory SHALL use restrictive permissions

The application SHALL create its state and config directory with permissions that prevent other local users from reading persisted credentials or configuration (mode 0700).

#### Scenario: First run creates the config directory
- **WHEN** the application starts and the config directory does not exist
- **THEN** the directory is created with restrictive permissions so other local users cannot read it

### Requirement: The root module SHALL contain only meaningful module replaces

The root product SHALL remove replace directives that are byte-identical to their upstream module and SHALL retain only documented root-product fork patches. This requirement applies only to the root module; the reference tree is excluded from cleanup scope.

#### Scenario: Root build uses the declared module graph
- **WHEN** `go build ./...` is run from the repository root
- **THEN** the root product builds with its declared module graph and no cleanup task requires compiling or modifying a reference tree

#### Scenario: Replace directive patches nothing
- **WHEN** a replace directive points at a fork whose content is byte-identical to the upstream module
- **THEN** the directive is removed and the build resolves the upstream module directly

### Requirement: Code generation SHALL be checksum-verified

`go:generate` steps that download external data SHALL verify the downloaded content against a pinned checksum before overwriting local files, and SHALL fail the generation step on mismatch.

#### Scenario: Download source is tampered
- **WHEN** a `go:generate` download in `internal/esim/pki` does not match its pinned checksum
- **THEN** generation fails and the previously generated files are left unchanged

### Requirement: Release build scripts SHALL run in a safe and reproducible workspace

The release build script SHALL build in a dedicated temporary directory created with `mktemp`, SHALL validate the VERSION argument against a strict format before using it in file or package names, and SHALL require the version to be supplied by the caller instead of falling back to a hardcoded value.

#### Scenario: Unsafe version is supplied
- **WHEN** the release build is invoked with a VERSION that contains characters outside the accepted format
- **THEN** the script rejects the version before writing any file

#### Scenario: Build root is fresh
- **WHEN** the release build starts
- **THEN** it creates a fresh temporary build directory via `mktemp` and never removes a pre-existing shared directory

### Requirement: Dead-state subsystems SHALL be removed

Subsystems with no production writer or caller SHALL be removed rather than retained: the IMSI reader registry in `pkg/logger`, and the legacy `AcquireSession`/`AcquireOneShot` APDU arbiter interfaces with their compatibility fields.

#### Scenario: Dead state is searched for
- **WHEN** the repository is searched for the removed subsystems after cleanup
- **THEN** no production code references the IMSI registry or the legacy arbiter session interfaces
