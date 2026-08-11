# macOS Code Map

## Product Path

The product has one macOS application process. `cmd/djonehub/main.go` starts the Go service and the native UI.

The main goroutine locks to the OS main thread. The native UI runs on that thread. The HTTP server runs on worker goroutines.

Do not start the native UI from a worker goroutine. Do not make a second notifier process.

`internal/platform/darwin/adapter.go` owns macOS discovery and USB AT transport setup. It accepts `2ca3:4006` for the DJI module and `2c7c:0125` for the Quectel module. It uses USB location, USB identity, and IMEI to create the stable device identity. `internal/modem.Manager` and `internal/backend.ATBackend` own the shared AT command session and modem operations.

## Module Owners

| Path | Owner | Main responsibility |
| --- | --- | --- |
| `internal/platform/darwin/` | macOS adapter | USB discovery, libusb AT transport, and network data |
| `internal/platform/native/` | Go bridge | C ABI calls, native UI lifecycle, and native commands |
| `macos/DJOneHubNotifier/` | Swift library | AppKit UI, notification delivery, menu bar, and panels |
| `internal/application/notification/` | Notification service | Event baseline, de-duplication, and sink calls |
| `internal/platform/startup/` | Login startup | User LaunchAgent state |
| `scripts/build-macos-dev.sh` | Development package | Local test application build |
| `scripts/build-macos.sh` | Release package | App, DMG, and SHA-256 file build |

## Bridge Path

```text
Runtime and application event
        |
notification.Service
        |
internal/platform/native
        |
bridge.h C ABI
        |
DJOneHubNotifier Swift library
```

The Swift library is a UI host. The Swift library does not poll the HTTP API. The Swift library does not implement Go business rules.

The reverse command path is:

```text
Swift user action
        |
native command callback
        |
native.Bridge command queue
        |
internal/app nativeCommandHandler
        |
application service and bridge result event
```

`reject_call` uses the calls service. `open_dashboard` returns the local web URL to the Swift library.

When an event, command, or sink method changes, update these items together:

- `docs/native-bridge-contract.md`
- `internal/platform/native/bridge.h`
- The Go bridge and its tests
- The Swift DTO or UI code and its tests
- `internal/application/notification/testdata/`

## Primary Tests and Builds

- Run `go test ./internal/platform/native ./internal/application/notification ./internal/platform/darwin` for focused bridge work.
- Run `swift test` in `macos/DJOneHubNotifier` for Swift work.
- Run `./scripts/build-macos-dev.sh` for a local application build.
- Use `./scripts/build-macos.sh <arm64|universal> <version>` for a release package.

The normal Go test build uses a USB stub. A macOS package build uses the `libusb` build tag and the packaged library.

## Native UI Fault Checks

| Symptom | First files | Primary tests |
| --- | --- | --- |
| The app starts without a native UI | `main.go`, `bridge.go`, `bridge_darwin.go` | `internal/platform/native/bridge_test.go` |
| The app hangs at startup | `main.go`, `bridge.go`, Swift `NativeUIHost.swift` | Bridge tests and `swift test` |
| A notification does not show | Notification service, `bridge.go`, `NativeNotificationService.swift` | Notification service and Swift bridge tests |
| A panel shows wrong data | `BridgeModels.swift`, `NativeUIHost.swift` | `BridgeEventTests.swift` |
| A native command has no effect | `bridge.go`, `app.go` | Bridge and notification contract tests |
| Login startup is wrong | `internal/platform/startup/` | `startup_darwin_test.go` |
