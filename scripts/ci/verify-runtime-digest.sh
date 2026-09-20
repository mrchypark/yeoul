#!/usr/bin/env bash

# Verify a downloaded native runtime asset against the committed digest pin in
# runtime-digests.txt before it is extracted or staged. This is fail-closed: an
# asset with no committed pin, or bytes that do not match the pin, is rejected.
#
# usage: verify-runtime-digest.sh <release-ref> <asset-name> <file>

set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <release-ref> <asset-name> <file>" >&2
  exit 2
fi

release_ref="$1"
asset_name="$2"
asset_path="$3"

script_dir="$(CDPATH='' cd "$(dirname "$0")" && pwd)"
digests_file="${YEOUL_RUNTIME_DIGESTS:-${script_dir}/runtime-digests.txt}"

if [[ ! -f "${digests_file}" ]]; then
  echo "runtime digest pin file not found: ${digests_file}" >&2
  exit 1
fi

expected=""
while read -r ref name digest _rest; do
  if [[ -z "${ref}" || "${ref}" == \#* ]]; then
    continue
  fi
  if [[ "${ref}" == "${release_ref}" && "${name}" == "${asset_name}" ]]; then
    expected="${digest}"
    break
  fi
done < "${digests_file}"

if [[ -z "${expected}" ]]; then
  echo "no committed digest pin for ${release_ref}/${asset_name}" >&2
  echo "refusing to stage unverified native bytes; add a line to ${digests_file}" >&2
  exit 1
fi

if [[ ! -f "${asset_path}" ]]; then
  echo "asset to verify is missing: ${asset_path}" >&2
  exit 1
fi

actual=""
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${asset_path}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${asset_path}" | awk '{print $1}')"
elif command -v openssl >/dev/null 2>&1; then
  actual="$(openssl dgst -sha256 "${asset_path}" | awk '{print $NF}')"
else
  echo "no SHA-256 tool available (need sha256sum, shasum, or openssl)" >&2
  echo "refusing to stage unverified native bytes" >&2
  exit 1
fi

actual="$(printf '%s' "${actual}" | tr 'A-Z' 'a-z')"
expected="$(printf '%s' "${expected}" | tr 'A-Z' 'a-z')"

if [[ "${actual}" != "${expected}" ]]; then
  echo "digest mismatch for ${asset_name} (${release_ref})" >&2
  echo "  expected ${expected}" >&2
  echo "  actual   ${actual}" >&2
  echo "refusing to stage replaced or corrupted native bytes" >&2
  exit 1
fi

printf 'verified %s (%s) sha256=%s\n' "${asset_name}" "${release_ref}" "${actual}"
