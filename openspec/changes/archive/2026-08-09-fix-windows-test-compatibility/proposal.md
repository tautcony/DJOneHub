## Why

The root Go test suite does not currently pass reliably on Windows: several tests assume case-sensitive environment variables, extensionless executables, and Linux-valid colon path components, while two connection/logging tests have timing-sensitive cleanup. These failures obscure real regressions and prevent Windows from being a verified development target.

## What Changes

- Make proxy-environment precedence tests independent of the host operating system's environment-key semantics.
- Make EDL runner fixtures create executable names that Windows `LookPath` can resolve.
- Skip Linux sysfs filesystem-shape tests on hosts that cannot represent Linux sysfs path names while retaining platform-independent Linux adapter tests.
- Remove the WebSocket test's invalid repeated-read-after-timeout pattern and synchronize event publication safely.
- Harden logger test cleanup so rotating log handles and stable-link files are released before temporary-directory removal.
- Run repeated package tests and the full `go test ./...` suite on Windows.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-adapters`: Extend supported-platform verification to require that the repository test suite uses host-valid fixtures and passes on Windows without attempting to create Linux-only filesystem names.

## Impact

Changes are limited to test seams and resource cleanup around `cmd/djonehub`, firmware runner resolution tests, Linux adapter tests, WebSocket tests, and logger lifecycle code/tests. Production proxy parsing, EDL behavior, platform capability reporting, and public APIs remain unchanged.
