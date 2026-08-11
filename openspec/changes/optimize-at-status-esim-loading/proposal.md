## Why

AT status and eSIM reads currently compete for one device command session. A
single page load can repeat modem queries, trigger both lightweight and rich
eSIM scans, and make a local traffic request enter the eSIM path. This produces
multi-second requests even when the device is ready and returns sensitive EID
data in the general homepage status surface.

## What Changes

- Forward the rich eSIM snapshot capability through the business adapter so the
  eSIM overview uses one full snapshot path when it needs rich fields.
- Add short-lived, generation-scoped Manager status caches for identity, radio,
  and SIM reads, with bounded in-flight coalescing.
- Keep ordinary SIM status independent from EID reads.
- Make eSIM health reuse cached device and eSIM snapshots.
- Stage the initial homepage requests and coalesce pending/history notification
  reads.
- Preserve discovered eUICC AIDs until a reset or validated target failure
  requires one static fallback scan.
- Log AT command queue wait and execution durations without logging command
  payloads or sensitive response data.
- Remove EID from the homepage device-status projection. Keep EID visible in
  the eSIM view and eSIM API response.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `device-services`: Status reads SHALL use bounded caches and shall not force
  eSIM EID discovery for ordinary SIM state.
- `esim-management-workbench`: eSIM reads SHALL share snapshots, preserve
  validated discovered targets, and expose EID only in the eSIM surface.
- `modem-backends`: AT command diagnostics SHALL record queue wait and execute
  durations without sensitive command content.
- `vue-management-ui`: Homepage loading SHALL be staged and homepage device
  status SHALL omit EID while eSIM views retain it.

## Impact

Affected code includes `internal/modem/`, `internal/backend/`,
`internal/application/device/`, `internal/application/network/`,
`internal/application/esim/`, `internal/esim/`, `internal/api/http/`, and
`web/src/`. The public eSIM response keeps its existing EID field. The public
device-status response remains schema-compatible but masks or omits EID in the
homepage projection. Hardware behavior is unchanged; only request scheduling,
cache reuse, and diagnostics change.
