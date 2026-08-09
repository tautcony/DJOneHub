## Context

The Go test suite contains fixtures that model Unix behavior directly through the
host filesystem and environment. Windows treats environment names as
case-insensitive, resolves executables through `PATHEXT`, and rejects colons in
file names, so those fixtures fail before the behavior under test is reached.
Separately, the WebSocket test continues reading after a terminal Gorilla
WebSocket read error, and file-rotatelogs dispatches rotation handlers in
untracked goroutines that can outlive a test temporary directory.

The change must keep production proxy, firmware, WebSocket, and Linux adapter
behavior intact while making Windows a reliable host for the repository suite.

## Goals / Non-Goals

**Goals:**

- Express proxy precedence through an injectable environment lookup so the test
  can model distinct upper- and lower-case keys on every host.
- Use host-resolvable executable fixtures and skip only tests whose Linux sysfs
  directory shape cannot be represented by Windows.
- Read a WebSocket connection only once after installing its deadline.
- Ensure the initial asynchronous log-rotation handler completes before its
  rotator and temporary directory are discarded.
- Verify affected packages repeatedly and run the full suite on Windows.

**Non-Goals:**

- Changing proxy precedence or modem/firmware runtime behavior.
- Emulating Linux sysfs semantics on Windows.
- Replacing Gorilla WebSocket or file-rotatelogs.
- Broad logger architecture changes unrelated to rotation shutdown.

## Decisions

### Inject environment lookup at the parsing boundary

`inspectProxyEnvironment` will retain its existing public behavior and delegate
to an internal function accepting a `getenv` callback. The precedence test can
then use a case-sensitive map regardless of host rules. A process-wide
environment mutation cannot represent two differently cased keys on Windows,
and subprocess testing would add complexity without improving coverage.

### Make fixtures host-valid instead of changing runtime lookup

The EDL test will create `uv.cmd` on Windows and `uv` elsewhere. The three tests
that require colon-bearing Linux sysfs interface paths will explicitly skip on
Windows; the remaining Linux adapter tests continue to run. Translating sysfs
names would test a filesystem shape that Linux never exposes and could conceal
path parsing defects.

### Treat a WebSocket read timeout as terminal

The server test will publish events from a bounded background loop and perform
one read with one deadline. Gorilla WebSocket documents read errors as terminal,
so retrying `ReadMessage` after timeout is invalid even when the connection was
healthy before the deadline.

### Track the initial rotation callback through logger shutdown

The composed rotation handler will expose completion of its first callback.
Logger setup will retain that handler next to the active rotator, and replacement
or test shutdown will close the writer and wait when a file was created. This
covers the initial stable-link callback that races temporary-directory cleanup.
Windows stable-link replacement will also be serialized and use rename after
creating the replacement hard link.

## Risks / Trade-offs

- [Risk] Waiting for a callback could block if no rotation event exists.
  -> Only wait when the rotator reports a current file, which means its initial
  rotation event was dispatched.
- [Risk] Skipped sysfs tests reduce Windows-host coverage of Linux-only paths.
  -> Keep all platform-independent adapter tests enabled and rely on Linux CI for
  native sysfs shape coverage.
- [Risk] A single-completion signal does not track every future compression
  callback during long-running rotation.
  -> Scope shutdown synchronization to the initial stable-link event responsible
  for the observed setup/test race; retain existing rotation semantics otherwise.

## Migration Plan

No data or configuration migration is required. Apply the test-seam and lifecycle
changes, run affected packages with repeated counts, then run `go test ./...` on
Windows. Rollback consists of reverting this isolated change.

## Open Questions

None.
