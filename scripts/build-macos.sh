#!/bin/sh
set -eu

# Builds the unified macOS app: the native UI static library is compiled
# first, then linked into the Go binary via cgo, then wrapped into a
# DJOneHub.app test bundle (single process, single LaunchAgent).
#
# Distribution (ZIP/DMG) is handled by package-macos-arm64.sh / universal;
# this script is for local development.

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="${ROOT_DIR}/dist"
APP="${DIST_DIR}/DJOneHub.app"

mkdir -p "${DIST_DIR}"

cd "${ROOT_DIR}"

echo "==> 1/3 构建 macOS 原生 UI 静态库"
"${ROOT_DIR}/macos/DJOneHubNotifier/build-app.sh" >/dev/null

echo "==> 2/3 构建 Go 主程序（链接原生 UI）"
ARCH=$(go env GOARCH)
PKG_CONFIG_PATH="${PKG_CONFIG_PATH:-/opt/homebrew/lib/pkgconfig:/usr/local/lib/pkgconfig}"
export PKG_CONFIG_PATH

CGO_ENABLED=1 GOOS=darwin GOARCH="${ARCH}" go build \
	-a \
	-p 2 \
  -trimpath -ldflags="-s -w" \
  -o "${DIST_DIR}/djonehub-macos-${ARCH}" ./cmd/djonehub

cp "${DIST_DIR}/djonehub-macos-${ARCH}" "${DIST_DIR}/djonehub-macos"

echo "==> 3/3 组装 DJOneHub.app（测试产物）"
rm -rf "${APP}"
mkdir -p "${APP}/Contents/MacOS" "${APP}/Contents/Resources"
cp "${DIST_DIR}/djonehub-macos-${ARCH}" "${APP}/Contents/MacOS/djonehub"
cp "${ROOT_DIR}/scripts/Info.plist" "${APP}/Contents/Info.plist"
chmod 755 "${APP}/Contents/MacOS/djonehub"
codesign --force --deep --sign - "${APP}" >/dev/null 2>&1 || true

echo "macOS binaries written to ${DIST_DIR}"
echo "app bundle: ${APP}"
