#!/bin/sh
set -eu

# Builds the universal DMG (arm64 + x86_64). The native UI is linked into the
# Go binary by package-macos-universal.sh (dual-arch Swift static library +
# lipo); there is no separate notifier app anymore.

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${1:-v0.1.3-preview}
DMG_NAME="DJOneHub-macOS-universal-${VERSION}.dmg"
STAGE="${ROOT_DIR}/dist/dmg-stage-universal"
DMG="${ROOT_DIR}/dist/${DMG_NAME}"

echo "==> 1/2 构建通用主程序（arm64 + x86_64，含原生 UI）"
"${ROOT_DIR}/scripts/package-macos-universal.sh" "${VERSION}"

echo "==> 2/2 组装安装目录并生成 DMG"
rm -rf "${STAGE}"
mkdir -p "${STAGE}"
ditto --norsrc --noextattr --noqtn --noacl "${ROOT_DIR}/dist/release/DJOneHub-macOS-universal-${VERSION}" "${STAGE}/djonehub"
cp "${ROOT_DIR}/scripts/dmg/安装 DJOneHub.command" "${STAGE}/安装 DJOneHub.command"
cp "${ROOT_DIR}/scripts/dmg/卸载 DJOneHub.command" "${STAGE}/卸载 DJOneHub.command"
cp "${ROOT_DIR}/scripts/dmg/使用说明.txt" "${STAGE}/使用说明.txt"
chmod 755 "${STAGE}/安装 DJOneHub.command" "${STAGE}/卸载 DJOneHub.command"

rm -f "${DMG}"
hdiutil create -volname "DJOneHub" -srcfolder "${STAGE}" -ov -format UDZO "${DMG}"
hdiutil verify "${DMG}"

echo
echo "完成：${DMG}"
