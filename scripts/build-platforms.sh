#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="${ROOT_DIR}/dist"
mkdir -p "${DIST_DIR}"
cd "${ROOT_DIR}"

build_one() {
	goos="$1"
	goarch="$2"
	output="${DIST_DIR}/djonehub-${goos}-${goarch}"
	CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags="-s -w" -o "${output}" ./cmd/djonehub
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "${output}" > "${output}.sha256"
	else
		sha256sum "${output}" > "${output}.sha256"
	fi
}

build_one linux amd64
build_one darwin arm64
build_one windows amd64

if command -v syft >/dev/null 2>&1; then
	syft "dir:${ROOT_DIR}" -o "spdx-json=${DIST_DIR}/djonehub.sbom.json"
	printf '%s\n' "SBOM written to ${DIST_DIR}/djonehub.sbom.json"
else
	printf '%s\n' "SBOM skipped: install syft to generate SPDX output" >&2
fi

printf '%s\n' "Cross-platform binaries and checksums written to ${DIST_DIR}"
