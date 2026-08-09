# DJOneHub Agent Guide

## Project Purpose and Scope

DJOneHub is a local, single-device management application for DJI/Quectel cellular modules. It manages the device through USB, serial, QMI, or MBIM and provides a Vue 3 UI served by a Go backend. It is not a device-pool product and must not claim a capability that has not been verified on the active platform, backend, and hardware.

- Primary target: DJI `2ca3:4006` and Quectel `2c7c:0125` USB identities.
- The runtime manages one physical device. A USB re-enumeration must reconnect to the same device using physical location, USB identity, and modem identity.
- The local control plane exposes sensitive data and destructive actions. Treat IMEI, ICCID, EID, IMSI, MSISDN, SMS bodies, activation codes, logs, and hardware captures as sensitive. Never add real-device data to fixtures, tests, documentation, commits, or output.

## Repository Navigation

- Use `docs/code-map/` as the primary navigation index.
- Read `README.md` for product scope, supported platforms, and commands.
- Read `SOURCE_STRUCTURE.md` for ownership and dependency boundaries.
- Read the relevant OpenSpec contract before changing specified behavior.
- Keep this file focused on durable repository guidance. Do not add completed work notes or temporary paths.

## Read Before Editing

Start with the relevant source and contract rather than guessing:

- Project overview, supported platforms, commands: `README.md`.
- Ownership and dependency boundaries: `SOURCE_STRUCTURE.md`.
- API and event contracts: `openspec/specs/device-api/spec.md` and `docs/native-bridge-contract.md`.
- Capability-driven UI: `openspec/specs/vue-management-ui/spec.md`.
- Backend and platform capability contracts: `openspec/specs/modem-backends/spec.md` and `openspec/specs/platform-adapters/spec.md`.
- macOS UI/build work: `MACOS.md` and `docs/MACOS_GO_NATIVE_BRIDGE_PLAN.md`.

For a non-trivial feature, behavior change, cross-layer contract, or architectural refactor, use the repository OpenSpec workflow before implementation. Keep the proposal, design, tasks, and delta specs consistent with the code. Small, localized bug fixes may proceed directly when they do not change a specified contract.

Write repository guidance and code-map files in clear, controlled English. Apply ASD-STE100 principles. Use short sentences, active voice, consistent technical names, and vertical lists for complex information.

## Architecture Boundaries

- `cmd/djonehub/` is the only product service entry point. Do not introduce a parallel macOS-only server or a second management UI.
- `internal/domain/` contains protocol- and platform-independent models, capabilities, errors, and business rules.
- `internal/application/` implements device, SMS, eSIM, network, AT, VoWiFi, and notification use cases. Keep HTTP and platform details out of it.
- `internal/backend/` owns AT/QMI/MBIM contracts and adapters. A backend must report its real capability set; unsupported work returns a structured unsupported error, never a fabricated success.
- `internal/runtime/` owns the single-device lifecycle, resource ownership, and event publication. Preserve cancellation, shutdown, locking, and operation-conflict behavior when changing it.
- `internal/platform/` owns OS discovery and transports. Platform code registers verified capabilities only.
- `internal/api/http/` translates between the local API and application services. Preserve `/api/v1` schemas and structured errors (`code`, `message`, `retryable`, optional `details`).
- `internal/storage/` owns SQLite/local state. Card-side eSIM profile data and local notes/labels are different sources of truth and must remain explicitly labelled in UI and API behavior.
- `web/` is the sole management UI: Vue 3, TypeScript, Vite, Pinia, and Ant Design Vue. Keep API calls in `web/src/services/`, domain state in typed stores/context, and avoid putting new business behavior into browser OS checks.

## Language And Documentation

- Write new and modified repository guidance and code-map text in clear, controlled English that follows ASD-STE100 principles.
- Use one instruction or fact per sentence. Use a specific subject and a precise verb.
- Use `must` for requirements, `may` for permission, and `can` for capability.
- Use the same term for the same concept. Define abbreviations before first use unless the codebase defines them.
- Keep user-facing Chinese strings as valid UTF-8. Do not add mojibake examples to tests or documentation.
- Apply the same rules to user-facing documentation. Keep required product names, identifiers, commands, paths, and localized strings unchanged.

## Capabilities, Events, and UI

- The server capability snapshot is authoritative. Gate navigation, controls, and requests by capabilities, not by OS name, USB mode name, or assumptions about a backend.
- Report unverified or unavailable functionality truthfully, with a structured reason and a usable UI explanation.
- Long-running mutations return operation IDs. Preserve operation progress, final state, and error propagation rather than assuming synchronous completion.
- WebSocket connections begin from a full snapshot. Events are ordered by runtime event ID; clients must resynchronize after a gap or out-of-order event.
- A new backend or platform adapter is incomplete until discovery, transport, capability declaration, API behavior, event behavior, and UI gating have all been validated.
- Keep sensitive identifiers masked by default in the UI. Do not expose SMS bodies, activation credentials, or other sensitive content in new logs or notifications without an explicit product decision.

## Device and Concurrency Safety

- Use a real device only for read-only verification unless the user explicitly authorizes a state-changing action. Never use a fake transport as evidence that a hardware feature works.
- Do not infer eSIM, SIM, VoWiFi, or network state from a single status field. Follow the relevant service/adapter contract and retain timeout, cancellation, and recovery paths.
- Preserve device-level arbitration for AT, APDU, SIM authentication, eSIM operations, and mode switching. Do not add an independent transport path that bypasses the established locks/barriers.
- All device-controlled lengths, counts, offsets, and fragmented payloads require bounds checks before allocation or iteration. Bound caches, operation histories, goroutines, retries, and reassembly/fragment collectors.
- Prefer typed/sentinel errors and `errors.Is` over matching error text. Pass `context.Context` through device work and give externally visible operations finite, intentional deadlines.

## OpenSpec Workflow

- OpenSpec specifications are under `openspec/specs/`. Active changes are under `openspec/changes/`.
- Before spec-driven implementation, discover the change with `openspec list --json`.
- Inspect it with `openspec status --change "<change-name>" --json` and validate it with `openspec validate "<change-name>" --strict`.
- Keep proposal, design, tasks, and delta specifications consistent with the implementation.
- Before archiving a change, sync every delta specification into its main specification.
- After archiving, run `openspec validate --all`.

## macOS Native UI

The macOS UI is a single-process bridge, not an independent notifier service:

- Swift static library: `macos/DJOneHubNotifier`; C ABI: `internal/platform/native/bridge.h`; Go bridge: `internal/platform/native/`.
- The main goroutine is locked to the OS main thread before starting the native UI. `Bridge.Start` synchronously runs the AppKit loop; HTTP runs on worker goroutines. Do not move this without changing the documented thread contract.
- Application events flow through `internal/application/notification.Service` into the native bridge. Swift is a presentation host and must not poll HTTP, deduplicate events, or recreate Go business logic.
- Any change to a bridge event, command, or `notification.Sink` must update the C header, Swift DTOs/UI, Go implementation, fixtures, contract documentation, and tests together. `HideCall` must retain the call payload used to match the active call.
- Use the macOS build scripts; do not hand-assemble an app bundle or bypass the libusb/signing/package validation.

## Implementation and Verification

- Make the smallest cohesive change. Do not refactor unrelated code or undo existing user changes.
- Reuse repository patterns and interfaces. Avoid compatibility shims, dead fallback paths, or duplicate implementations unless a supported migration explicitly needs one.
- Add focused tests for changed behavior, especially capability declarations, structured errors, event schemas, parser bounds, lifecycle transitions, and concurrency-sensitive code.
- For Go changes, run `go test ./...` (or explain why the environment cannot run it). Use `go test -race` for changed synchronization/lifecycle code when practical.
- For frontend changes, run the relevant checks:

  ```sh
  npm --prefix web run typecheck
  npm --prefix web run lint
  npm --prefix web run build
  ```

- Do not read source or configuration files as text to test their content. Use compiled behavior, structured APIs, integration checks, or runtime verification.
- Preserve test isolation. Do not run concurrent commands that share build or generated outputs when they can lock files.
- Never run destructive device commands, delete user data, or alter eSIM profiles, USB mode, firmware, network registration, or SIM state without explicit user authorization.

- For macOS packaging changes, also run the appropriate build script and validate the generated bundle/DMG as described in `MACOS.md`. Ordinary Go tests intentionally use the non-libusb stub.

## Git and Documentation

- Preserve a dirty worktree. The current `docs/outdated/` migration and any other uncommitted changes belong to the user unless they explicitly say otherwise.
- Treat `docs/outdated/` as read-only historical material. Do not modify files in that directory unless the user explicitly asks for that work.
- Do not commit unless asked. When asked to commit, omit any `Co-Authored-By` trailer unless the user explicitly requests it.
- Update user-facing docs when support coverage, local data behavior, API/UI capability behavior, macOS packaging, or safety boundaries change. Mark superseded design documents as outdated rather than letting them compete with current OpenSpec contracts.
- Update the applicable files in `docs/code-map/` when feature work changes module ownership, an entry point, runtime wiring, or primary tests.

## PowerShell Guidance

- On Windows, prefer `pwsh.exe` over `powershell.exe` unless Windows PowerShell 5.1 is required.
- For native commands, store the executable path and each argument as separate values. Use `&` to run the command and capture `$LASTEXITCODE` immediately.
- Use PowerShell cmdlets for file operations. Use `-LiteralPath` for real paths and specify UTF-8 when reading or writing text.
- Use a temporary `.ps1` file for multiline scripts, complex quoting, JSON, XML, regular expressions, or non-ASCII paths.
