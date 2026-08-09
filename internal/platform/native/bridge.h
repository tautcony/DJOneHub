// The C ABI between the Go main process and the Swift native UI layer.
// Symbols are implemented in Swift (NativeUIHost.swift, exported with
// @_cdecl) and linked from macos/DJOneHubNotifier/.build/release/
// libDJOneHubNotifier.a.
//
// Threading contract:
//   - native_ui_start must be called from the process main thread. The Go
//     side guarantees this: cmd/djonehub/main.go calls runtime.LockOSThread()
//     on the main goroutine before any goroutine scheduling, and bridge.Start
//     blocks on the main thread. The Swift entry point checks Thread.isMainThread
//     explicitly and fails with a readable error if the contract is violated.
//     It blocks until the UI run loop exits.
//   - native_ui_handle_event may be called from any thread; the UI dispatches
//     to the main thread internally.
//   - on_command and on_ready fire on the main thread while the run loop is
//     running; Go handles them on fresh goroutines.
#ifndef DJONEHUB_NATIVE_BRIDGE_H
#define DJONEHUB_NATIVE_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

typedef void (*native_ui_command_cb)(char *command_json);
typedef void (*native_ui_ready_cb)(void);

// Starts the native UI host on the calling thread and blocks until the UI run
// loop exits. config_json is a small JSON object, currently {"web_url": "..."}.
// on_command receives user actions as JSON matching the notification.Command
// contract. on_ready fires once the UI finished launching.
void native_ui_start(const char *config_json, native_ui_command_cb on_command, native_ui_ready_cb on_ready);

// Delivers one bridge event (runtime.Event JSON) to the UI.
void native_ui_handle_event(const char *event_json);

// Requests a clean shutdown of the UI run loop; native_ui_start then returns.
void native_ui_stop(void);

#ifdef __cplusplus
}
#endif

#endif /* DJONEHUB_NATIVE_BRIDGE_H */
