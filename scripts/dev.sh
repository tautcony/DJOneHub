#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
"${ROOT_DIR}/scripts/dev-backend.sh" "$@" &
BACKEND_PID=$!
trap 'kill "${BACKEND_PID}" 2>/dev/null || true' INT TERM EXIT

"${ROOT_DIR}/scripts/dev-web.sh"
