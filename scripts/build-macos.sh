#!/bin/sh
set -eu

# Build one macOS application and its DMG. The universal variant is built on
# Apple Silicon by compiling both Go and libusb slices, then combining them.

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ARCH=${1:-arm64}
VERSION=${2:-}
REDOWNLOAD=0
case "${VERSION}" in
"") printf '%s\n' "Usage: $0 <arm64|universal> <version> [--redownload]" >&2; exit 2 ;;
esac
case "${3:-}" in
--redownload) REDOWNLOAD=1 ;;
"") ;;
*) printf '%s\n' "Usage: $0 <arm64|universal> <version> [--redownload]" >&2; exit 2 ;;
esac
case "${ARCH}" in
arm64|universal) ;;
*) printf '%s\n' "Usage: $0 <arm64|universal> <version> [--redownload]" >&2; exit 2 ;;
esac
# VERSION is caller-supplied and must match ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$.
# The pattern rejects path separators, whitespace, and PlistBuddy separators,
# so the value is safe to embed in PACKAGE_NAME / dist paths and PlistBuddy keys.
if ! printf '%s' "${VERSION}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	printf '%s\n' "VERSION '${VERSION}' does not match ^v[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?\$" >&2
	exit 2
fi

if [ "${ARCH}" = "arm64" ] && [ "$(uname -m)" != "arm64" ]; then
	printf '%s\n' "The arm64 package must be built on Apple Silicon." >&2
	exit 1
fi
for command in go curl npm swift clang codesign hdiutil; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		printf '%s\n' "${command} is required to build the macOS package." >&2
		exit 1
	fi
done
if [ "${ARCH}" = "universal" ] && ! command -v lipo >/dev/null 2>&1; then
	printf '%s\n' "lipo is required to build the universal package." >&2
	exit 1
fi

DIST_DIR="${ROOT_DIR}/dist"
LIBUSB_VERSION=1.0.30
LIBUSB_SHA256=fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf
LIBUSB_URL="https://github.com/libusb/libusb/releases/download/v${LIBUSB_VERSION}/libusb-${LIBUSB_VERSION}.tar.bz2"
LIBUSB_CACHE_DIR="${DIST_DIR}/cache/libusb"
LIBUSB_ARCHIVE="${LIBUSB_CACHE_DIR}/libusb-${LIBUSB_VERSION}.tar.bz2"
PACKAGE_NAME="DJOneHub-macOS-${ARCH}-${VERSION}"
STAGE_DIR="${DIST_DIR}/release/${PACKAGE_NAME}"
APP_DIR="${STAGE_DIR}/DJOneHub.app"
APP_BINARY="${APP_DIR}/Contents/MacOS/djonehub"
DMG_STAGE="${DIST_DIR}/dmg-stage-${ARCH}"
DMG="${DIST_DIR}/${PACKAGE_NAME}.dmg"
CHECKSUM="${DMG}.sha256"
# Fresh temporary build root: mktemp guarantees a private directory that was
# never touched by a previous build, so stale artifacts cannot leak in and no
# pre-existing shared directory is ever removed.
BUILD_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/djonehub-macos-${ARCH}.XXXXXX")
LIBUSB_SOURCE="${BUILD_ROOT}/libusb-source"
LIBUSB_INCLUDE="${BUILD_ROOT}/libusb-include"
SWIFT_BUILD_ROOT="${BUILD_ROOT}/swift-build"
SWIFT_CACHE="${BUILD_ROOT}/swift-cache"
trap 'rm -rf "${BUILD_ROOT}"' EXIT INT TERM

rm -rf "${STAGE_DIR}" "${DMG_STAGE}"
mkdir -p "${APP_DIR}/Contents/MacOS" "${APP_DIR}/Contents/Resources" "${APP_DIR}/Contents/lib" "${STAGE_DIR}/licenses" "${LIBUSB_CACHE_DIR}"

if [ "${REDOWNLOAD}" -eq 1 ] || [ ! -f "${LIBUSB_ARCHIVE}" ]; then
	printf '%s\n' "Downloading libusb ${LIBUSB_VERSION}..."
	curl -fL "${LIBUSB_URL}" -o "${LIBUSB_ARCHIVE}.tmp"
	mv "${LIBUSB_ARCHIVE}.tmp" "${LIBUSB_ARCHIVE}"
else
	printf '%s\n' "Using cached libusb archive: ${LIBUSB_ARCHIVE}"
fi
ACTUAL_SHA256=$(shasum -a 256 "${LIBUSB_ARCHIVE}" | awk '{print $1}')
if [ "${ACTUAL_SHA256}" != "${LIBUSB_SHA256}" ]; then
	printf '%s\n' "libusb source checksum mismatch; use --redownload to fetch it again." >&2
	exit 1
fi

mkdir -p "${LIBUSB_SOURCE}"
tar -xjf "${LIBUSB_ARCHIVE}" -C "${LIBUSB_SOURCE}" --strip-components=1
(
	cd "${LIBUSB_SOURCE}"
	MACOSX_DEPLOYMENT_TARGET=13.0 ./configure \
		--prefix="${BUILD_ROOT}/libusb-prefix" \
		--disable-static --enable-shared --disable-dependency-tracking >/dev/null
	sed -i '' 's/#define HAVE_PIPE2 1/\/\* #undef HAVE_PIPE2 \*\//' config.h
)
mkdir -p "${LIBUSB_INCLUDE}/libusb-1.0"
ln -s "${LIBUSB_SOURCE}/libusb/libusb.h" "${LIBUSB_INCLUDE}/libusb-1.0/libusb.h"

LIBUSB_SOURCES="\
libusb/core.c \
libusb/descriptor.c \
libusb/hotplug.c \
libusb/io.c \
libusb/strerror.c \
libusb/sync.c \
libusb/os/events_posix.c \
libusb/os/threads_posix.c \
libusb/os/darwin_usb.c"

build_libusb() {
	arch=$1
	out=$2
	objects="${BUILD_ROOT}/libusb-${arch}-objects"
	mkdir -p "${objects}" "${out}/lib"
	for source in ${LIBUSB_SOURCES}; do
		object="${objects}/$(basename "${source}" .c).o"
		clang -arch "${arch}" -mmacosx-version-min=13.0 -DHAVE_CONFIG_H \
			-I"${LIBUSB_SOURCE}" -I"${LIBUSB_SOURCE}/libusb" -fPIC \
			-c "${LIBUSB_SOURCE}/${source}" -o "${object}"
	done
	clang -arch "${arch}" -mmacosx-version-min=13.0 -dynamiclib \
		-install_name "@executable_path/../lib/libusb-1.0.0.dylib" \
		-compatibility_version 7.0.0 -current_version 7.0.0 \
		-o "${out}/lib/libusb-1.0.0.dylib" "${objects}"/*.o \
		-framework IOKit -framework CoreFoundation -framework Security -lobjc
	ln -sfn libusb-1.0.0.dylib "${out}/lib/libusb-1.0.dylib"
}

cd "${ROOT_DIR}"
npm --prefix web run build

if [ "${ARCH}" = "arm64" ]; then
	LIBUSB_ARM="${BUILD_ROOT}/libusb-arm64"
	build_libusb arm64 "${LIBUSB_ARM}"
	NOTIFIER_SRC="${ROOT_DIR}/macos/DJOneHubNotifier"
	(
		cd "${NOTIFIER_SRC}"
		mkdir -p "${SWIFT_CACHE}/clang" "${SWIFT_CACHE}/swiftpm"
		CLANG_MODULE_CACHE_PATH="${SWIFT_CACHE}/clang" \
		SWIFTPM_MODULECACHE_OVERRIDE="${SWIFT_CACHE}/clang" \
		SWIFTPM_CUSTOM_CACHE_PATH="${SWIFT_CACHE}/swiftpm" \
		swift build --disable-sandbox -c release --scratch-path "${SWIFT_BUILD_ROOT}"
		mkdir -p "${NOTIFIER_SRC}/.build/release"
		cp "${SWIFT_BUILD_ROOT}/release/libDJOneHubNotifier.a" "${NOTIFIER_SRC}/.build/release/libDJOneHubNotifier.a"
	)
	GOCACHE="${BUILD_ROOT}/go-cache"; rm -rf "${GOCACHE}"; mkdir -p "${GOCACHE}"
	GOCACHE="${GOCACHE}" CGO_CFLAGS="-I${LIBUSB_INCLUDE}" \
		CGO_LDFLAGS="-L${LIBUSB_ARM}/lib" \
		MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		go build -tags libusb -p 2 -trimpath -buildvcs=false -ldflags="-s -w" -o "${APP_BINARY}" ./cmd/djonehub
	cp "${LIBUSB_ARM}/lib/libusb-1.0.0.dylib" "${APP_DIR}/Contents/lib/libusb-1.0.0.dylib"
else
	LIBUSB_ARM="${BUILD_ROOT}/libusb-arm64"
	LIBUSB_X86="${BUILD_ROOT}/libusb-x86_64"
	build_libusb arm64 "${LIBUSB_ARM}"
	build_libusb x86_64 "${LIBUSB_X86}"
	lipo -create "${LIBUSB_ARM}/lib/libusb-1.0.0.dylib" "${LIBUSB_X86}/lib/libusb-1.0.0.dylib" \
		-output "${APP_DIR}/Contents/lib/libusb-1.0.0.dylib"

	NOTIFIER_SRC="${ROOT_DIR}/macos/DJOneHubNotifier"
	mkdir -p "${SWIFT_CACHE}/clang" "${SWIFT_CACHE}/swiftpm"
	export CLANG_MODULE_CACHE_PATH="${SWIFT_CACHE}/clang"
	export SWIFTPM_MODULECACHE_OVERRIDE="${SWIFT_CACHE}/clang"
	export SWIFTPM_CUSTOM_CACHE_PATH="${SWIFT_CACHE}/swiftpm"
	(
		cd "${NOTIFIER_SRC}"
		swift build --disable-sandbox -c release --scratch-path "${SWIFT_BUILD_ROOT}"
		cp "${SWIFT_BUILD_ROOT}/release/libDJOneHubNotifier.a" "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a"
		swift build --disable-sandbox -c release --arch x86_64 \
			--scratch-path "${SWIFT_BUILD_ROOT}" \
			-Xswiftc -runtime-compatibility-version -Xswiftc none
		cp "${SWIFT_BUILD_ROOT}/release/libDJOneHubNotifier.a" "${BUILD_ROOT}/libDJOneHubNotifier-x86_64.a"
		cp "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a" "${SWIFT_BUILD_ROOT}/release/libDJOneHubNotifier.a"
	)
	mkdir -p "${NOTIFIER_SRC}/.build/release"
	lipo -create "${BUILD_ROOT}/libDJOneHubNotifier-arm64.a" "${BUILD_ROOT}/libDJOneHubNotifier-x86_64.a" \
		-output "${NOTIFIER_SRC}/.build/release/libDJOneHubNotifier.a"
	build_go() {
		arch=$1
		cache="${BUILD_ROOT}/go-cache-${arch}"
		rm -rf "${cache}"; mkdir -p "${cache}"
		if [ "${arch}" = "arm64" ]; then libdir="${LIBUSB_ARM}/lib"; else libdir="${LIBUSB_X86}/lib"; fi
		CGO_CFLAGS="-I${LIBUSB_INCLUDE}" CGO_LDFLAGS="-L${libdir}" GOCACHE="${cache}" \
			MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOOS=darwin GOARCH="${arch}" \
			go build -tags libusb -p 2 -trimpath -buildvcs=false -ldflags="-s -w" \
			-o "${BUILD_ROOT}/djonehub-${arch}" ./cmd/djonehub
	}
	build_go arm64
	build_go amd64
	lipo -create "${BUILD_ROOT}/djonehub-arm64" "${BUILD_ROOT}/djonehub-amd64" -output "${APP_BINARY}"
fi

cp "${ROOT_DIR}/scripts/Info.plist" "${APP_DIR}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION#v}" "${APP_DIR}/Contents/Info.plist"
mkdir -p "${APP_DIR}/Contents/Resources/web/dist"
cp -R "${ROOT_DIR}/web/dist/." "${APP_DIR}/Contents/Resources/web/dist/"
cp "${ROOT_DIR}/LICENSE" "${STAGE_DIR}/LICENSE"
cp "${LIBUSB_SOURCE}/COPYING" "${STAGE_DIR}/licenses/libusb-COPYING"
cp "${ROOT_DIR}/packaging/THIRD_PARTY_NOTICES.md" "${STAGE_DIR}/THIRD_PARTY_NOTICES.md"
chmod 755 "${APP_BINARY}" "${APP_DIR}/Contents/lib/libusb-1.0.0.dylib"
codesign --force --deep --sign - "${APP_DIR}"
if otool -L "${APP_BINARY}" | grep -q '/opt/homebrew\|/usr/local\|/Cellar/'; then
	printf '%s\n' "Release binary still contains a package-manager dependency." >&2
	exit 1
fi
find "${STAGE_DIR}" -name '._*' -delete

mkdir -p "${DMG_STAGE}"
ditto --norsrc --noextattr --noqtn --noacl "${APP_DIR}" "${DMG_STAGE}/DJOneHub.app"
mkdir -p "${DMG_STAGE}/DJOneHub-licenses/licenses"
cp "${STAGE_DIR}/LICENSE" "${DMG_STAGE}/DJOneHub-licenses/"
cp "${STAGE_DIR}/THIRD_PARTY_NOTICES.md" "${DMG_STAGE}/DJOneHub-licenses/"
cp "${STAGE_DIR}/licenses/libusb-COPYING" "${DMG_STAGE}/DJOneHub-licenses/licenses/"
ln -s /Applications "${DMG_STAGE}/Applications"
hdiutil create -volname "DJOneHub" -srcfolder "${DMG_STAGE}" -ov -format UDZO "${DMG}"

# hdiutil can return before the newly created image is ready for a verify
# operation on busy macOS runners. Retry transient resource errors, but keep
# the verification failure fatal after a bounded wait.
verify_dmg() {
	verify_attempt=1
	verify_max_attempts=5
	while ! hdiutil verify "${DMG}"; do
		if [ "${verify_attempt}" -ge "${verify_max_attempts}" ]; then
			printf '%s\n' "DMG verification failed after ${verify_max_attempts} attempts." >&2
			return 1
		fi
		printf '%s\n' "DMG is not ready for verification; retrying (${verify_attempt}/${verify_max_attempts})..." >&2
		verify_attempt=$((verify_attempt + 1))
		sleep 2
	done
}
verify_dmg
shasum -a 256 "${DMG}" > "${CHECKSUM}"

printf '%s\n' "Application: ${APP_DIR}"
printf '%s\n' "DMG: ${DMG}"
printf '%s\n' "Checksum: ${CHECKSUM}"
