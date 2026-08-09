## Context

The serial AT manager currently classifies most lines beginning with `+` as URCs before considering whether they are the expected payload of the active command. Consequently, responses to polling commands such as `AT+CEREG?`, `AT+QSIMSTAT?`, and `AT+CPIN?` are logged and dispatched through the asynchronous state-change path. The SMS poller runs every three seconds and the network poller every fifteen seconds, so stable state generates a continuous stream of false changes.

The manager already serializes commands and owns both the active request and incoming line at the point where classification occurs. It also retains registration and SIM state fields that can serve as the baseline for genuine unsolicited transitions. Existing uncommitted raw-AT terminal-response work in the same file must remain intact.

## Goals / Non-Goals

**Goals:**

- Attribute expected status response lines to the active command rather than the URC dispatcher.
- Suppress duplicate unsolicited registration and SIM status reports.
- Keep the manager's observed state synchronized with successful polling results so a later identical unsolicited report is not treated as a transition.
- Preserve unrelated asynchronous URCs interleaved with commands.
- Document all recurring production tasks and identify which ones generate AT traffic.

**Non-Goals:**

- Changing polling intervals or removing polling services.
- Changing public APIs or modem backend interfaces.
- Deduplicating non-state URCs such as SMS, call, and USSD notifications.
- Replacing the AT parser or command serialization architecture.

## Decisions

### Classify against the active command before generic URC handling

The command loop will recognize payload prefixes derived from status query commands and append those matching lines to the current response without calling `handleURC`. This decision uses command context, which is the only reliable distinction between a synchronous response and an unsolicited line with the same textual prefix.

A global exclusion in `isURC` was rejected because it would also hide genuine `+CEREG`, `+QSIMSTAT`, and `+CPIN` events received while no matching command is active.

### Maintain an observed-state baseline

Successful registration and SIM queries will update the manager's cached observed state without publishing a change event. Genuine state URCs will compare their parsed value with this baseline under the existing information lock. Only an unknown initial value or a differing value is accepted as a transition; repeated values return before logging and callbacks.

Unknown/invalid parsed values will not replace a known baseline and will remain diagnosable without being presented as a confirmed change.

### Keep state-event side effects behind the transition gate

Logging, SIM status callbacks, and the `+CPIN: READY` reset signal will run only for accepted transitions. This prevents polling responses and repeated identical firmware reports from causing modem reset events or downstream re-probing.

## Risks / Trade-offs

- [A modem reboots and emits only `+CPIN: READY` while the cached state was already READY] -> The repeated value will be suppressed. Explicit `RDY`, serial failure/reconnect, and registration/SIM transitions remain reset evidence; tests will pin the requested transition-only behavior.
- [A genuine same-prefix URC arrives during its matching query] -> The serial protocol cannot distinguish two identical lines without modem-specific framing. Treating it as the query response is safe because the queried value updates the same observed-state baseline.
- [Command-prefix matching becomes too broad] -> Restrict matching to exact known status queries and exact response prefixes, with tests proving unrelated URCs still dispatch.

## Migration Plan

No data migration is required. Deploy the command classification and state tracking together. Rollback consists of reverting the manager and its tests; polling and public contracts are unchanged.

## Open Questions

None.
