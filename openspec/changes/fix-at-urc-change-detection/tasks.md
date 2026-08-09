## 1. Operational Documentation

- [x] 1.1 Document recurring backend, connection-maintenance, conditional, and frontend timers under `docs`, including their device-access impact.
- [x] 1.2 Document the false URC trigger chain and identify the SMS and network pollers that generate the observed status traffic.

## 2. AT Response Classification

- [x] 2.1 Keep `+CEREG`, `+QSIMSTAT`, and `+CPIN` payloads with their matching active query instead of routing them through `handleURC`.
- [x] 2.2 Preserve unrelated asynchronous URC dispatch while a status query is active.

## 3. State Transition Filtering

- [x] 3.1 Update the observed registration and SIM baselines from successful synchronous queries.
- [x] 3.2 Suppress repeated unsolicited registration/SIM values before logging, callbacks, reset dispatch, or follow-up handling.
- [x] 3.3 Preserve one-time handling for genuine registration and SIM transitions.

## 4. Verification

- [x] 4.1 Add focused unit tests for synchronous response isolation, unrelated URC interleaving, duplicate suppression, and genuine transitions.
- [x] 4.2 Run modem package tests and validate the OpenSpec change.
