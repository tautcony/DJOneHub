#!/bin/sh
set -eu

# Builds the arm64 DMG. The native UI is linked into the Go binary by
# package-macos-arm64.sh; there is no separate notifier app anymore.

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${1:-v0.1.1-preview}
DMG_NAME="DJOneHub-macOS-arm64-${VERSION}.dmg"
STAGE="${ROOT_DIR}/dist/dmg-stage"
DMG="${ROOT_DIR}/dist/${DMG_NAME}"

echo "==> 1/3 构建 Go 主程序（含原生 UI 与 libusb）"
"${ROOT_DIR}/scripts/package-macos-arm64.sh" "${VERSION}"

echo "==> 2/3 组装安装目录"
rm -rf "${STAGE}"
mkdir -p "${STAGE}"
ditto --norsrc --noextattr --noqtn --noacl "${ROOT_DIR}/dist/release/DJOneHub-macOS-arm64-${VERSION}" "${STAGE}/djonehub"
cp "${ROOT_DIR}/scripts/dmg/安装 DJOneHub.command" "${STAGE}/安装 DJOneHub.command"
cp "${ROOT_DIR}/scripts/dmg/卸载 DJOneHub.command" "${STAGE}/卸载 DJOneHub.command"
cp "${ROOT_DIR}/scripts/dmg/使用说明.txt" "${STAGE}/使用说明.txt"
chmod 755 "${STAGE}/安装 DJOneHub.command" "${STAGE}/卸载 DJOneHub.command"

echo "==> 3/3 生成 DMG"
rm -f "${DMG}"
hdiutil create -volname "DJOneHub" -srcfolder "${STAGE}" -ov -format UDZO "${DMG}"
hdiutil verify "${DMG}"

echo
echo "完成：${DMG}"
