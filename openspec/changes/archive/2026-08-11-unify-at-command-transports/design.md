## Context

Linux and Windows discover an operating-system serial port. `ATFactory` creates `modem.Manager`, and `ATBackend` exposes modem operations. macOS discovers the same module through IOKit but opens its AT interface through libusb. The macOS adapter currently creates `CommandBackend`, which duplicates AT queries, SMS work, APDU work, response handling, and capability decisions.

`modem.Manager` already owns the required shared behavior. It serializes commands, separates unsolicited result codes from command responses, handles interactive prompts, bounds line input, quarantines late responses after a timeout, owns APDU leases, and publishes modem reset events. Its command loop only needs a byte reader, byte writer, and closer. Serial-port opening is the only direct operating-system dependency in its startup path.

The current `macos-command-backend-sim-auth` change adds SIM authentication to `CommandBackend`. This change preserves that behavior through the existing `ATBackend` and `modem.Manager` SIM authentication implementation.

## Goals / Non-Goals

**Goals:**

- Use `modem.Manager` and `ATBackend` for macOS, Linux, and Windows AT devices.
- Define a small transport contract for the shared AT command session.
- Let `modem.Manager` either open a configured serial port or use an injected transport.
- Make the macOS libusb transport implement the shared stream contract.
- Use one device APDU arbiter for AT SIM authentication and eSIM operations on every platform.
- Remove `CommandBackend` and its transport-specific command interfaces.
- Preserve platform discovery, physical identity, close, reconnect, and capability behavior.

**Non-Goals:**

- Merge QMI or Mobile Broadband Interface Model backends into the AT session.
- Change the HTTP API, WebSocket event schema, or frontend behavior.
- Add a new hardware capability.
- Change Linux or Windows device discovery.
- Claim successful macOS hardware verification from unit tests.

## Decisions

### Inject a stream transport into `modem.Manager`

Add an exported `ATTransport` interface with `Read`, `Write`, and `Close`. Add a constructor that creates a manager with an already-open transport. Keep the existing constructor and serial open path for Linux and Windows.

This approach reuses the complete AT state machine. A command-only adapter would keep response collection and unsolicited result code handling inside the macOS transport and would preserve the current semantic split.

### Keep transport opening in the platform boundary

`ATFactory` accepts an optional platform transport opener. A candidate with an operating-system AT port uses the existing serial open path. A candidate without an AT port uses the platform opener and injects the returned transport into `modem.Manager`.

The macOS adapter returns the selected libusb transport. It does not construct a modem backend or eSIM service.

### Use one backend construction path

`ATFactory` creates the manager, APDU arbiter, `ATBackend`, eSIM port, and `BusinessAdapter` for every AT transport. The composition root installs the same eSIM port builder for macOS, Linux, and Windows.

This decision keeps capability derivation and legacy backend access consistent. It also removes the direct-backend special case from VoWiFi tests and comments.

### Preserve the macOS probe before session startup

The macOS transport can use its bounded synchronous command helper before it is injected. The shared manager owns all reads and writes after startup. No code can call the synchronous helper after manager startup.

### Keep read deadlines transport-specific

The shared manager optionally calls `SetReadTimeout` when a transport provides it. Serial ports and the macOS USB transport use a short read timeout so shutdown and command dispatch remain responsive.

## Risks / Trade-offs

- [Risk] A libusb read can delay a command write while both operations use one handle lock. -> Keep the USB read timeout short and use separate command-session serialization in the manager.
- [Risk] The shared backend can expose a capability that was not verified on macOS. -> Preserve the existing capability set and do not add a capability in this change. Run the macOS package build and require real-device verification before expanding support claims.
- [Risk] Manager initialization sends more setup commands than the old macOS backend. -> Keep the existing bounded initialization sequence and treat a rejected optional command as non-fatal.
- [Risk] A transport timeout can be mistaken for a disconnect. -> Return a timeout error that the manager read loop recognizes and continues.
- [Risk] Removing `CommandBackend` can break tests and VoWiFi unwrapping assumptions. -> Migrate tests to the shared backend and keep the narrow backend unwrapping contract based on interfaces, not concrete types.

## Migration Plan

1. Add the transport interface and injected-transport manager constructor.
2. Add focused manager tests for injected transport startup, command execution, and close.
3. Change `ATFactory` to build the same manager and backend for serial and injected transports.
4. Change the macOS USB implementation and adapter to expose the stream transport.
5. Install the common eSIM builder on all three platforms.
6. Remove `CommandBackend` and migrate affected tests and documentation.
7. Run OpenSpec validation, focused Go tests, the full Go suite, race tests for the changed session, and the macOS build check that the environment supports.

Rollback restores the macOS adapter to `CommandBackend`. Linux and Windows remain on the unchanged serial manager path.

## Open Questions

None.
