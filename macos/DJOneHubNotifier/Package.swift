// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "DJOneHubNotifier",
    platforms: [.macOS(.v13)],
    products: [
        // Static library linked into the Go main process via cgo; exposes the
        // C ABI declared in internal/platform/darwin/native/bridge.h.
        .library(name: "DJOneHubNotifier", type: .static, targets: ["DJOneHubNotifier"]),
        // Dev-only CLI for the bridge self-test; not part of the
        // distribution (see docs/MACOS_GO_NATIVE_BRIDGE_PLAN.md phase 4).
        .executable(name: "DJOneHubNotifierCLI", targets: ["DJOneHubNotifierCLI"]),
    ],
    targets: [
        .target(
            name: "DJOneHubNotifier",
            path: "Sources/DJOneHubNotifier"
        ),
        .executableTarget(
            name: "DJOneHubNotifierCLI",
            dependencies: ["DJOneHubNotifier"],
            path: "Sources/DJOneHubNotifierCLI"
        ),
        .testTarget(
            name: "DJOneHubNotifierTests",
            dependencies: ["DJOneHubNotifier"],
            path: "Tests/DJOneHubNotifierTests"
        ),
    ]
)
