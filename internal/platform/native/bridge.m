// The native_ui_* symbols declared in bridge.h are implemented in Swift
// (macos/DJOneHubNotifier/Sources/DJOneHubNotifier/NativeUIHost.swift) and
// linked in from the static library. This translation unit exists so cgo
// compiles bridge.h with clang, validating the interface contract.
#import "bridge.h"
