#!/bin/sh
set -eu

# Build the local macOS app used by scripts/dev-backend.sh. Release builds use
# scripts/build-macos.sh instead.
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="${ROOT_DIR}/dist"
APP="${DIST_DIR}/DJOneHub.app"
mkdir -p "${DIST_DIR}"
cd "${ROOT_DIR}"

"${ROOT_DIR}/macos/DJOneHubNotifier/build-app.sh" >/dev/null
ARCH=$(go env GOARCH)
CGO_CFLAGS="${CGO_CFLAGS:--I/opt/homebrew/include -I/usr/local/include}" \
CGO_LDFLAGS="${CGO_LDFLAGS:--L/opt/homebrew/lib -L/usr/local/lib -lusb-1.0}" \
	CGO_ENABLED=1 GOOS=darwin GOARCH="${ARCH}" go build -a -p 2 -trimpath -ldflags="-s -w" \
	-o "${DIST_DIR}/djonehub-macos-${ARCH}" ./cmd/djonehub

rm -rf "${APP}"
mkdir -p "${APP}/Contents/MacOS" "${APP}/Contents/Resources"
cp "${DIST_DIR}/djonehub-macos-${ARCH}" "${APP}/Contents/MacOS/djonehub"
cp "${ROOT_DIR}/scripts/Info.plist" "${APP}/Contents/Info.plist"
chmod 755 "${APP}/Contents/MacOS/djonehub"
codesign --force --deep --sign - "${APP}" >/dev/null 2>&1 || true
printf '%s\n' "App bundle: ${APP}"
