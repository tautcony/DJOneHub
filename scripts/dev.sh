#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# Resolve the backend listen address the same way dev-backend.sh does, so the
# readiness probe polls the port the backend will actually bind. A `-listen`
# flag passed through (scripts/dev.sh -listen 127.0.0.1:7575) wins over the
# DJONEHUB_LISTEN default.
LISTEN=${DJONEHUB_LISTEN:-127.0.0.1:7575}
prev=
for arg in "$@"; do
	if [ "${prev}" = "-listen" ]; then
		LISTEN=${arg}
		break
	fi
	prev=${arg}
done
BACKEND_HOST=$(printf '%s' "${LISTEN}" | sed 's/:\([0-9][0-9]*\)$//; s/^\[//; s/\]$//')
BACKEND_PORT=$(printf '%s' "${LISTEN}" | sed 's/^.*://')

"${ROOT_DIR}/scripts/dev-backend.sh" "$@" &
BACKEND_PID=$!
trap 'kill "${BACKEND_PID}" 2>/dev/null || true' INT TERM EXIT

# Wait until the backend is actually serving before starting the frontend, so
# the first proxied request never races a not-yet-ready server. On macOS this
# also covers the app-bundle build dev-backend.sh runs first.
port_open() {
	if command -v nc >/dev/null 2>&1; then
		nc -z -w 1 "${BACKEND_HOST}" "${BACKEND_PORT}" >/dev/null 2>&1
		return $?
	fi
	if command -v python3 >/dev/null 2>&1; then
		python3 - "${BACKEND_HOST}" "${BACKEND_PORT}" <<'EOF'
import socket, sys
s = socket.socket()
s.settimeout(1)
try:
    s.connect((sys.argv[1], int(sys.argv[2])))
    sys.exit(0)
except OSError:
    sys.exit(1)
finally:
    s.close()
EOF
		return $?
	fi
	echo "dev.sh: need nc or python3 for the backend readiness probe" >&2
	return 1
}

backend_alive() {
	kill -0 "${BACKEND_PID}" 2>/dev/null || return 1
	# An exited-but-unreaped child still answers kill -0 as a zombie; treat it
	# as dead so a crashed backend does not stall the probe for the timeout.
	case "$(ps -o stat= -p "${BACKEND_PID}" 2>/dev/null || true)" in
	Z*) return 1 ;;
	esac
	return 0
}

tries=0
while [ "${tries}" -lt 300 ]; do
	if backend_alive && port_open; then
		printf '%s\n' "Backend ready on http://${LISTEN}"
		break
	fi
	if ! backend_alive; then
		echo "dev.sh: backend exited before becoming ready" >&2
		wait "${BACKEND_PID}" 2>/dev/null || true
		exit 1
	fi
	tries=$((tries + 1))
	if [ $((tries % 15)) -eq 0 ]; then
		echo "dev.sh: still waiting for backend on ${LISTEN} (${tries}s)..." >&2
	fi
	sleep 1
done
if [ "${tries}" -ge 300 ]; then
	echo "dev.sh: backend did not become ready on ${LISTEN} within 300s" >&2
	exit 1
fi

"${ROOT_DIR}/scripts/dev-web.sh"
