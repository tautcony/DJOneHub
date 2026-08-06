#!/bin/sh
# Refresh the embedded PKI dictionaries (ci.json, accredited.json) from
# https://euicc-manual.osmocom.org. Each download is pinned to a SHA-256:
# the file is fetched to a .tmp sibling, verified against the pinned value,
# and only then atomically renamed into place. A mismatch aborts the
# generation and leaves the committed files untouched.
#
# To refresh the checksums deliberately:
#   1. Review the upstream source is still the intended one.
#   2. Download the new files and verify they are well-formed JSON.
#   3. Update CI_SHA256 / ACCREDITED_SHA256 below to the new values, and
#      update CI_SHA256_SOURCE / ACCREDITED_SHA256_SOURCE with a short note
#      of why the data was refreshed.
#   4. Run `go generate ./internal/esim/pki/...` and commit the new data
#      files together with the checksum change.
set -eu

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

CI_URL="https://euicc-manual.osmocom.org/docs/pki/ci/manifest.json"
ACCREDITED_URL="https://euicc-manual.osmocom.org/docs/pki/eum/accredited.json"

# CI_SHA256 was last verified 2026-08-06 (no upstream change since commit).
CI_SHA256="bb3ffc095a6e92ff616f2dd7ab660f792a2c463c2cc5578909f796d7647f2601"
# ACCREDITED_SHA256 was deliberately refreshed 2026-08-06: upstream grew from
# 32 to 37 suppliers (5 new suppliers; CEC Huada gained a locations entry).
ACCREDITED_SHA256="ad2095df4ed935abe554bc512e442237775f418e97d9ac6029b58e69712a78ef"

fetch_and_verify() {
	url=$1
	sha256=$2
	target=$3
	tmp="${target}.tmp"
	curl -fsSL "${url}" -o "${tmp}"
	actual=$(shasum -a 256 "${tmp}" | awk '{print $1}')
	if [ "${actual}" != "${sha256}" ]; then
		rm -f "${tmp}"
		printf '%s\n' "checksum mismatch for ${target}: got ${actual}, want ${sha256}; " \
			"data was not refreshed (see update_pki_data.sh for how to update the pin deliberately)" >&2
		exit 1
	fi
	mv "${tmp}" "${target}"
	printf '%s\n' "refreshed ${target} (sha256 ${actual})"
}

fetch_and_verify "${CI_URL}" "${CI_SHA256}" "${DIR}/ci.json"
fetch_and_verify "${ACCREDITED_URL}" "${ACCREDITED_SHA256}" "${DIR}/accredited.json"
