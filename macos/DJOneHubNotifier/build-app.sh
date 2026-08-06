#!/bin/zsh

# Builds the native UI static library that the Go main process links via cgo,
# then runs the development CLI self-test. The resulting static library is
# linked into the unified DJOneHub.app by the macOS packaging scripts.

set -eu

root="${0:A:h}"
build_root="${root}/.build"
cache_root="${build_root}/local-cache"
build_root_marker="${build_root}/.djonehub-build-root"

cd "${root}"
# Swift and Clang build artifacts contain absolute source paths. Clear the
# package build directory only when this checkout has moved.
cached_root=""
if [[ -f "${build_root_marker}" ]]; then
  cached_root="$(<"${build_root_marker}")"
fi
if [[ "${cached_root}" != "${root}" ]]; then
  rm -rf "${build_root}"
  mkdir -p "${build_root}"
  print -r -- "${root}" > "${build_root_marker}"
fi
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
