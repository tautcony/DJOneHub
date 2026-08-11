## 1. Shared AT Transport

- [x] 1.1 Add the minimal AT transport contract and injected-transport manager constructor.
- [x] 1.2 Make manager startup, health checks, and shutdown work with serial and injected transports.
- [x] 1.3 Add focused manager tests for injected transport command execution and ownership.

## 2. Unified Backend Wiring

- [x] 2.1 Change ATFactory to construct the shared manager and ATBackend for serial and injected transports.
- [x] 2.2 Change the macOS USB AT implementation and stub to implement the shared stream transport.
- [x] 2.3 Change the macOS adapter and composition root to use the shared factory and eSIM builder.

## 3. Duplicate Removal

- [x] 3.1 Remove CommandBackend and obsolete command-only transport contracts.
- [x] 3.2 Migrate backend, VoWiFi, and platform tests to the shared AT backend path.
- [x] 3.3 Update source comments and code-map documentation for the unified AT path.

## 4. Verification

- [x] 4.1 Run strict OpenSpec validation and focused Go tests.
- [x] 4.2 Run the full Go test suite and race tests for changed AT lifecycle code.
- [x] 4.3 Run the applicable macOS build validation or document the environment blocker.
