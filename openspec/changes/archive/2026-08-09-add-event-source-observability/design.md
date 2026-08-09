## Context

The backend already knows the main poller intervals, but the UI only displays a running-worker aggregate and a partial graph. Event-producing sources need an explicit contract so periodic modem and system observations can be distinguished from transport keepalive and cleanup machinery.

## Goals / Non-Goals

**Goals:**

- Identify event-producing workers in the diagnostics response.
- Describe their emitted event families and intervals.
- Show all event sources and connect them to the EventBus in the topology.

**Non-Goals:**

- Instrumenting WebSocket/SSE keepalive or retry timers as event sources.
- Changing poll intervals or event publishing semantics.
- Showing every conditional operation timer as a worker.

## Decisions

Add an `event_source` flag and `event_types` list to worker diagnostics. Populate it only for device discovery, backend event consumption, SMS, network, traffic, and call sources. The frontend will filter this metadata into a compact source list while the existing topology remains the event-flow visualization. Add explicit traffic and backend-event topology nodes and edges.

## Risks / Trade-offs

- [Risk] Event-type metadata can drift from implementation. -> Keep the list close to the existing worker definitions and cover the response shape in HTTP tests.
- [Risk] A second list duplicates topology nodes. -> Use the same worker diagnostics metadata for both views and keep topology focused on relationships.

## Migration Plan

The response fields are additive and optional for clients. Deploy backend and frontend together, then run Go and frontend type/build checks.

## Open Questions

None.
