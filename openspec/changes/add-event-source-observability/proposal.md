## Why

The runtime diagnostics page currently exposes aggregate worker counts and a partial static topology, so operators cannot see every periodic task that produces domain events or which event types each source emits.

## What Changes

- Mark event-producing runtime workers explicitly and expose their event types.
- Add missing traffic and backend-event source nodes and edges to the runtime topology.
- Render an event-source list with state, interval, and emitted event types in the Runtime view.
- Keep transport maintenance, cleanup, retry, and UI refresh timers out of the event-source view.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `device-events`: Runtime diagnostics SHALL expose and render periodic domain-event sources.
- `vue-management-ui`: The Runtime view SHALL show event-source details separately from mechanism workers and channels.

## Impact

Changes affect the runtime diagnostics response, topology construction, TypeScript diagnostics types, RuntimeView rendering, and localized labels. No event publishing behavior or public device API changes.
