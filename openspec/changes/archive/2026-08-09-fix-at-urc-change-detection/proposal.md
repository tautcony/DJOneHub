## Why

Periodic AT status queries currently route `+CEREG`, `+QSIMSTAT`, and `+CPIN` response lines through the unsolicited-result-code handler. Stable polling is therefore logged and dispatched as registration or SIM changes, creating false reset/change signals and unnecessary follow-up work even when the modem and SIM have not changed.

## What Changes

- Keep response lines that belong to the active AT command in that command's response stream instead of dispatching them as URCs.
- Track the last observed registration and SIM states and suppress repeated unsolicited state reports whose values did not change.
- Preserve genuine asynchronous URCs and only publish registration/SIM state changes after a real transition is observed.
- Document the repository's recurring background tasks, intervals, device access, and their relationship to AT status traffic.
- Add regression tests for synchronous response isolation and repeated-state suppression.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `modem-backends`: Clarify that synchronous AT response lines must not be emitted as unsolicited state changes and repeated state URCs must not trigger change handling.

## Impact

The change affects the serial AT command dispatcher and modem state tracking in `internal/modem`, focused tests for AT execution/URC behavior, and a new operational note under `docs`. Public HTTP and backend interfaces remain unchanged.
