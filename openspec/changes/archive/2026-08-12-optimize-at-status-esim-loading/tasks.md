## 1. Backend Snapshot Contracts

- [x] 1.1 Forward `ESIMSnapshotPort` through `BusinessAdapter` and add adapter contract tests.
- [x] 1.2 Add generation-scoped, TTL-bound identity/radio/SIM caches with in-flight coalescing to the device service.
- [x] 1.3 Remove EID discovery from ordinary `BusinessAdapter.SIM` and add regression coverage.
- [x] 1.4 Reuse device and eSIM snapshots in `/api/v1/esim/health` and omit EID from the homepage device projection.

## 2. eSIM Discovery and API Loading

- [x] 2.1 Preserve validated discovered AIDs across cancelled reads and invalidate them only on reset, reconnect, card change, or validated target failure.
- [x] 2.2 Stage homepage loading after device status and coalesce pending/history notification reads in the eSIM store.
- [x] 2.3 Keep EID visible in the eSIM view while removing it from homepage status presentation.

## 3. AT Diagnostics

- [x] 3.1 Record safe AT command class, queue wait, execution duration, terminal result, and timeout class in Manager diagnostics.
- [x] 3.2 Add tests that verify timing fields and sensitive command/response data are excluded.

## 4. Verification

- [x] 4.1 Run focused Go tests for adapter, device, eSIM, modem, and API behavior.
- [x] 4.2 Run frontend typecheck, lint, and build checks.
- [x] 4.3 Run full Go tests, race tests for changed lifecycle/cache code, strict OpenSpec validation, and diff checks.
