## Why

DJOneHub currently uses one AT command session for Linux and Windows serial ports and a separate command implementation for the macOS USB bulk transport. The two implementations duplicate modem operations and apply different timeout, error, prompt, and unsolicited result code behavior.

## What Changes

- Add one transport-independent AT command session for serial and USB bulk transports.
- Use one AT backend for macOS, Linux, and Windows.
- Keep platform discovery, physical transport opening, physical transport recovery, and host network work in platform adapters.
- Preserve AT command serialization, unsolicited result code delivery, prompt handling, bounded response collection, timeout quarantine, and device reconnect behavior on each transport.
- Remove the duplicate macOS command backend after the shared backend provides equivalent verified capabilities.
- Keep QMI and Mobile Broadband Interface Model (MBIM) backends separate from the AT command session.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `modem-backends`: Require every AT transport to use one shared command-session contract and one AT business implementation.
- `platform-adapters`: Require each platform adapter to expose transport operations without duplicating AT command or modem business behavior.

## Impact

- Affected code includes `internal/modem/`, `internal/backend/`, `internal/platform/darwin/`, `internal/platform/linux/`, `internal/platform/windows/`, and runtime backend wiring.
- Existing `/api/v1` schemas and capability names do not change.
- macOS USB behavior changes where the old path returned a partial timeout response as success, treated modem error responses as success, or discarded unsolicited input before a command.
- Linux and Windows continue to use their serial ports and keep their current platform discovery behavior.
- Focused transport-session, backend, platform-wiring, timeout, prompt, unsolicited result code, and capability tests are required.
