//go:build darwin && cgo

package native

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo LDFLAGS: -L${SRCDIR}/../../../../macos/DJOneHubNotifier/.build/release -lDJOneHubNotifier -lc++
#include <stdlib.h>
#include "bridge.h"

// cgo generates these exact signatures for the //export trampolines below.
extern void goNativeCommandCallback(char *json);
extern void goNativeReadyCallback(void);
*/
import "C"

import (
	"sync"
	"unsafe"
)

// The Swift UI layer is linked as a static library from
// macos/DJOneHubNotifier/.build/release/libDJOneHubNotifier.a; the Swift
// runtime itself ships in macOS (10.14.4+). build-macos.sh compiles the
// library before invoking the Go build.

var (
	driverMu     sync.Mutex
	activeBridge *Bridge
)

type darwinDriver struct{}

func newDriver() uiDriver { return darwinDriver{} }

// start runs the Swift NSApplication run loop on the calling thread (the Go
// main goroutine, pinned with runtime.LockOSThread) until native_ui_stop
// terminates it. User actions arrive back through the exported trampolines.
func (d darwinDriver) start(configJSON string, bridge *Bridge) {
	driverMu.Lock()
	activeBridge = bridge
	driverMu.Unlock()
	cConfig := C.CString(configJSON)
	defer C.free(unsafe.Pointer(cConfig))
	C.native_ui_start(cConfig, C.native_ui_command_cb(C.goNativeCommandCallback), C.native_ui_ready_cb(C.goNativeReadyCallback))
	driverMu.Lock()
	activeBridge = nil
	driverMu.Unlock()
}

func (d darwinDriver) handleEvent(eventJSON string) {
	cEvent := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cEvent))
	C.native_ui_handle_event(cEvent)
}

func (d darwinDriver) stop() {
	C.native_ui_stop()
}

func (d darwinDriver) hasUI() bool { return true }

//export goNativeCommandCallback
func goNativeCommandCallback(jsonCString *C.char) {
	if jsonCString == nil {
		return
	}
	driverMu.Lock()
	bridge := activeBridge
	driverMu.Unlock()
	if bridge == nil {
		return
	}
	bridge.enqueueCommand(C.GoString(jsonCString))
}

//export goNativeReadyCallback
func goNativeReadyCallback() {
	driverMu.Lock()
	bridge := activeBridge
	driverMu.Unlock()
	if bridge == nil {
		return
	}
	select {
	case <-bridge.ready:
	default:
		close(bridge.ready)
	}
}
