#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec npm --prefix "${ROOT_DIR}/web" run dev -- --host 127.0.0.1 --port "${DJONEHUB_WEB_PORT:-5176}" "$@"
