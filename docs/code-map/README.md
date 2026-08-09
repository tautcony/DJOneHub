# DJOneHub Code Map

Use this directory as the primary navigation index for the current codebase.

Read a map before you search the full repository. Each map identifies the owner, the first source files, the runtime path, and the primary tests.

## Start Here

| When you change | Read |
| --- | --- |
| The Go startup path, HTTP API, device runtime, or services | [backend.md](backend.md) |
| The Vue application, navigation, API client, or stores | [frontend.md](frontend.md) |
| The macOS AppKit UI, Swift bridge, or app package | [macos.md](macos.md) |
| An HTTP route, event type, capability, or persistent record | [contracts.md](contracts.md) |
| A visible failure that you must trace | [diagnostics.md](diagnostics.md) |

## Navigation by Symptom

| Symptom | Read first | Then read |
| --- | --- | --- |
| The process does not start or stop | [backend.md](backend.md) | `cmd/djonehub/main.go`, `internal/app/app.go` |
| A module is not found | [diagnostics.md](diagnostics.md) | `internal/platform/<os>/adapter.go`, `internal/runtime/runtime.go` |
| The API has a wrong result | [contracts.md](contracts.md) | `internal/api/http/server.go`, the related service |
| A page is missing, disabled, or stale | [frontend.md](frontend.md) | `router.ts`, `device.ts`, [contracts.md](contracts.md) |
| An eSIM action fails or conflicts | [diagnostics.md](diagnostics.md) | `internal/esim/`, `internal/apduarbiter/` |
| A macOS panel or notice fails | [macos.md](macos.md) | `internal/platform/native/`, `macos/DJOneHubNotifier/` |

## Supporting Documents

- `README.md` gives the product scope and the build commands.
- `SOURCE_STRUCTURE.md` gives the directory boundaries.
- `openspec/specs/` gives the active behavior requirements.
- `docs/native-bridge-contract.md` gives the macOS bridge contract.

The code map gives fast navigation. The OpenSpec files and contracts define the required behavior.

## Update Rule

Update a code-map file when work changes a module owner, an entry point, runtime wiring, an HTTP route, an event, a capability, or a primary test.

Do not put design history in this directory. Put active behavior requirements in `openspec/specs/`. Put detailed implementation contracts near the related code or in `docs/`.
