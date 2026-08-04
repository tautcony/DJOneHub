#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${1:-dev}
PACKAGE_NAME="DJOneHub-macOS-universal-${VERSION}"
STAGE_ROOT="${ROOT_DIR}/dist/release"
STAGE_DIR="${STAGE_ROOT}/${PACKAGE_NAME}"
APP_DIR="${STAGE_DIR}/DJOneHub.app"
APP_BINARY="${APP_DIR}/Contents/MacOS/djonehub"
LIBUSB_VERSION=1.0.30
LIBUSB_SHA256=fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf
LIBUSB_URL="https://github.com/libusb/libusb/releases/download/v${LIBUSB_VERSION}/libusb-${LIBUSB_VERSION}.tar.bz2"
BUILD_ROOT="${TMPDIR:-/tmp}/djonehub-macos-package-universal"
LIBUSB_ARCHIVE="${BUILD_ROOT}/libusb-${LIBUSB_VERSION}.tar.bz2"
LIBUSB_SOURCE="${BUILD_ROOT}/libusb-source"
LIBUSB_ARM="${BUILD_ROOT}/libusb-arm64"
LIBUSB_X86="${BUILD_ROOT}/libusb-x86_64"
PC_SHIM="${BUILD_ROOT}/pc-shim"

if ! command -v go >/dev/null 2>&1; then echo "Go is required to build the release package." >&2; exit 1; fi
if ! command -v curl >/dev/null 2>&1; then echo "curl is required to download the official libusb source archive." >&2; exit 1; fi
if ! command -v pkg-config >/dev/null 2>&1; then echo "pkg-config is required on the build Mac." >&2; exit 1; fi
if ! command -v lipo >/dev/null 2>&1; then echo "lipo is required to build a universal package." >&2; exit 1; fi
if ! command -v npm >/dev/null 2>&1; then echo "npm is required to build the Vue management page." >&2; exit 1; fi

rm -rf "${STAGE_DIR}"
mkdir -p "${APP_DIR}/Contents/MacOS" "${APP_DIR}/Contents/Resources" "${APP_DIR}/Contents/lib" "${STAGE_DIR}/licenses"
rm -rf "${LIBUSB_ARM}" "${LIBUSB_X86}"
mkdir -p "${BUILD_ROOT}" "${LIBUSB_ARM}/lib" "${LIBUSB_X86}/lib"

if [ ! -f "${LIBUSB_ARCHIVE}" ]; then
  curl -fL "${LIBUSB_URL}" -o "${LIBUSB_ARCHIVE}"
fi
ACTUAL_SHA256=$(shasum -a 256 "${LIBUSB_ARCHIVE}" | awk '{print $1}')
if [ "${ACTUAL_SHA256}" != "${LIBUSB_SHA256}" ]; then
  echo "libusb source checksum mismatch." >&2
  exit 1
fi

rm -rf "${LIBUSB_SOURCE}"
mkdir -p "${LIBUSB_SOURCE}"
tar -xjf "${LIBUSB_ARCHIVE}" -C "${LIBUSB_SOURCE}" --strip-components=1
(
  cd "${LIBUSB_SOURCE}"
  MACOSX_DEPLOYMENT_TARGET=13.0 ./configure \
    --prefix="${BUILD_ROOT}/libusb-prefix" \
    --disable-static \
    --enable-shared \
    --disable-dependency-tracking >/dev/null
  sed -i '' 's/#define HAVE_PIPE2 1/\/\* #undef HAVE_PIPE2 \*\//' config.h
)
mkdir -p "${BUILD_ROOT}/libusb-prefix/include/libusb-1.0"
cp "${LIBUSB_SOURCE}/libusb/libusb.h" "${BUILD_ROOT}/libusb-prefix/include/libusb-1.0/libusb.h"

build_libusb() {
  arch=$1
  out=$2
  objects="${BUILD_ROOT}/libusb-${arch}-objects"
  mkdir -p "${objects}"
  for source in \
    libusb/core.c \
    libusb/descriptor.c \
    libusb/hotplug.c \
    libusb/io.c \
    libusb/strerror.c \
    libusb/sync.c \
    libusb/os/events_posix.c \
    libusb/os/threads_posix.c \
    libusb/os/darwin_usb.c
  do
    object="${objects}/$(basename "${source}" .c).o"
    clang -arch "${arch}" -mmacosx-version-min=13.0 -DHAVE_CONFIG_H \
      -I"${LIBUSB_SOURCE}" -I"${LIBUSB_SOURCE}/libusb" -fPIC \
      -c "${LIBUSB_SOURCE}/${source}" -o "${object}"
  done
  clang -arch "${arch}" -mmacosx-version-min=13.0 -dynamiclib \
    -install_name "@executable_path/../lib/libusb-1.0.0.dylib" \
    -compatibility_version 7.0.0 -current_version 7.0.0 \
    -o "${out}/lib/libusb-1.0.0.dylib" \
    "${objects}"/*.o \
    -framework IOKit -framework CoreFoundation -framework Security -lobjc
  ln -sfn libusb-1.0.0.dylib "${out}/lib/libusb-1.0.dylib"
}

build_libusb arm64 "${LIBUSB_ARM}"
build_libusb x86_64 "${LIBUSB_X86}"
lipo -create "${LIBUSB_ARM}/lib/libusb-1.0.0.dylib" "${LIBUSB_X86}/lib/libusb-1.0.0.dylib" \
  -output "${APP_DIR}/Contents/lib/libusb-1.0.0.dylib"

# x86_64 构建用 pkg-config shim，避免被系统 Homebrew 的 arm64 libusb 干扰
mkdir -p "${PC_SHIM}"
cat > "${PC_SHIM}/pkg-config" <<EOF
#!/bin/sh
case "\$*" in
  *libusb-1.0*)
    out=""
    if [ "\${PKG_ARCH:-arm64}" = "x86_64" ] || [ "\${PKG_ARCH:-arm64}" = "amd64" ]; then libdir="${LIBUSB_X86}/lib"; else libdir="${LIBUSB_ARM}/lib"; fi
    case "\$*" in *--cflags*) out="-I${LIBUSB_SOURCE}/libusb \$out" ;; esac
    case "\$*" in *--libs*) out="-L\${libdir} -lusb-1.0 \$out" ;; esac
    [ -n "\$out" ] && echo "\$out"
    exit 0
    ;;
esac
exec /usr/bin/pkg-config "\$@"
EOF
chmod 755 "${PC_SHIM}/pkg-config"

cd "${ROOT_DIR}"
# Build the native UI static library for both architectures and merge it into
# a universal libDJOneHubNotifier.a so each Go arch links the same fat file.
# SwiftPM writes single-arch builds to .build/release, so each slice is
# preserved under BUILD_ROOT before the next build.
NOTIFIER_SRC="${ROOT_DIR}/macos/DJOneHubNotifier"
SWIFT_CACHE="${BUILD_ROOT}/local-cache"
mkdir -p "${SWIFT_CACHE}/clang" "${SWIFT_CACHE}/swiftpm"
export CLANG_MODULE_CACHE_PATH="${SWIFT_CACHE}/clang"
export SWIFTPM_MODULECACHE_OVERRIDE="${SWIFT_CACHE}/clang"
export SWIFTPM_CUSTOM_CACHE_PATH="${SWIFT_CACHE}/swiftpm"
(
  cd "${NOTIFIER_SRC}"
  swift build --disable-sandbox -c release
  cp .build/release/libDJOneHubNotifier.a "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a"
  # x86_64 needs the runtime compatibility pack disabled so the Go cc link
  # can resolve the Swift runtime without the SwiftPM driver.
  swift build --disable-sandbox -c release --arch x86_64 \
    -Xswiftc -runtime-compatibility-version -Xswiftc none
  cp .build/release/libDJOneHubNotifier.a "${BUILD_ROOT}/libDJOneHubNotifier-x86_64.a"
  # Restore the arm64 slice for any later local builds.
  cp "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a" .build/release/libDJOneHubNotifier.a
)
lipo -create "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a" \
  "${BUILD_ROOT}/libDJOneHubNotifier-x86_64.a" \
  -output "${NOTIFIER_SRC}/.build/release/libDJOneHubNotifier.a"
file "${NOTIFIER_SRC}/.build/release/libDJOneHubNotifier.a" | cut -c1-120

build_go() {
  arch=$1
  cache="${BUILD_ROOT}/go-cache-${arch}"
  rm -rf "${cache}"
  mkdir -p "${cache}"
  PATH="${PC_SHIM}:$PATH" PKG_ARCH="${arch}" GOCACHE="${cache}" \
    PKG_CONFIG_PATH="" \
    MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOOS=darwin GOARCH="${arch}" \
    go build -p 2 -trimpath -buildvcs=false -ldflags="-s -w" \
    -o "${BUILD_ROOT}/djonehub-${arch}" ./cmd/djonehub
}

build_go arm64
build_go amd64
lipo -create "${BUILD_ROOT}/djonehub-arm64" "${BUILD_ROOT}/djonehub-amd64" \
  -output "${APP_BINARY}"

cd "${ROOT_DIR}"
npm --prefix web run build
mkdir -p "${APP_DIR}/Contents/Resources/web/dist"
cp -R "${ROOT_DIR}/web/dist/." "${APP_DIR}/Contents/Resources/web/dist/"

cp "${ROOT_DIR}/scripts/Info.plist" "${APP_DIR}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION#v}" "${APP_DIR}/Contents/Info.plist"
cp "${ROOT_DIR}/LICENSE" "${STAGE_DIR}/LICENSE"
cp "${LIBUSB_SOURCE}/COPYING" "${STAGE_DIR}/licenses/libusb-COPYING"
cp "${ROOT_DIR}/packaging/THIRD_PARTY_NOTICES.md" "${STAGE_DIR}/THIRD_PARTY_NOTICES.md"

chmod 755 "${APP_BINARY}" "${APP_DIR}/Contents/lib/libusb-1.0.0.dylib"
codesign --force --deep --sign - "${APP_DIR}"

if otool -L "${APP_BINARY}" | grep -q '/opt/homebrew\|/usr/local\|/Cellar/'; then
  echo "Release binary still contains a package-manager dependency." >&2
  exit 1
fi

find "${STAGE_DIR}" -name '._*' -delete

echo "Release directory: ${STAGE_DIR}"
