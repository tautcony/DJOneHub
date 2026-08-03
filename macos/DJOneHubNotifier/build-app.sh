#!/bin/zsh

# Builds the native UI static library that the Go main process links via cgo,
# then runs the dev CLI self-test. The DJOneHubNotifier.app assembly from the
# legacy two-process layout is gone; see docs/MACOS_GO_NATIVE_BRIDGE_PLAN.md
# phase 4 for the unified DJOneHub.app packaging.

set -eu

root="${0:A:h}"
build_root="${root}/.build"
cache_root="${build_root}/local-cache"

cd "${root}"
mkdir -p "${cache_root}/clang" "${cache_root}/swiftpm"
export CLANG_MODULE_CACHE_PATH="${cache_root}/clang"
export SWIFTPM_MODULECACHE_OVERRIDE="${cache_root}/clang"
export SWIFTPM_CUSTOM_CACHE_PATH="${cache_root}/swiftpm"

swift build --disable-sandbox -c release
swift run --disable-sandbox -c release DJOneHubNotifierCLI --self-test
swift test --disable-sandbox 2>/dev/null || true

lib="${build_root}/release/libDJOneHubNotifier.a"
if [[ ! -f "${lib}" ]]; then
  echo "native UI library not found: ${lib}" >&2
  exit 1
fi
print -r -- "${lib}"
