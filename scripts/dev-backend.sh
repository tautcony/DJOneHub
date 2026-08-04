#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "${ROOT_DIR}"

LISTEN=${DJONEHUB_LISTEN:-127.0.0.1:7576}

if [ "$(uname -s)" = "Darwin" ]; then
	# UserNotifications requires a real .app bundle. A raw `go run` executable
	# lives under /tmp/go-build and has no bundle identity.
	"${ROOT_DIR}/scripts/build-macos-dev.sh"
	exec "${ROOT_DIR}/dist/DJOneHub.app/Contents/MacOS/djonehub" \
		-listen "${LISTEN}" -web-dir "${ROOT_DIR}/web/dist" "$@"
fi

exec go run ./cmd/djonehub -listen "${LISTEN}" -web-dir "${ROOT_DIR}/web/dist" "$@"
