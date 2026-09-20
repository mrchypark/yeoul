#!/usr/bin/env bash
#
# Regression tests for the Homebrew publication policy in
# scripts/ci/publish-homebrew.sh and the serialization declared in
# .github/workflows/release.yml.
#
# The publisher runs for real against a local bare repository that stands in
# for the tap, with stub gh and curl binaries on PATH. This covers REL-06: a
# release job that reaches publication after a newer tag must not move the
# shared formula backwards, and the workflow must serialize publications on
# the destination formula instead of on the workflow ref.

set -euo pipefail

script_dir="$(CDPATH='' cd "$(dirname "$0")" && pwd)"
repo_root="$(CDPATH='' cd "${script_dir}/../.." && pwd)"
publisher_path="${repo_root}/scripts/ci/publish-homebrew.sh"
workflow_path="${repo_root}/.github/workflows/release.yml"

for required_file in "${publisher_path}" "${workflow_path}"; do
  if [[ ! -f "${required_file}" ]]; then
    echo "missing required file: ${required_file}" >&2
    exit 1
  fi
done

work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

checks=0
failures=0

ok() {
  printf '  ok   %s\n' "$1"
}

fail() {
  failures=$((failures + 1))
  printf '  FAIL %s\n' "$1"
  printf '       expected: %s\n' "$2"
  printf '       actual:   %s\n' "$3"
}

section() {
  printf '\n== %s ==\n' "$1"
}

assert_eq() {
  checks=$((checks + 1))
  if [[ "$2" == "$3" ]]; then
    ok "$1"
  else
    fail "$1" "$2" "$3"
  fi
}

assert_file_contains() {
  checks=$((checks + 1))
  if [[ -f "$2" ]] && grep -F -q -e "$3" "$2"; then
    ok "$1"
  else
    fail "$1" "contains: $3" "not found in $2"
  fi
}

# --- gh and curl stubs ----------------------------------------------------

stub_dir="${work_dir}/bin"
mkdir -p "${stub_dir}"

cat > "${stub_dir}/gh" <<'GH_STUB'
#!/bin/sh
set -eu
if [ "${1:-}" != "api" ]; then
  printf 'unexpected gh invocation: %s\n' "$*" >&2
  exit 1
fi
path="$2"
case "${path}" in
  repos/*/releases/tags/*)
    tag="${path##*/}"
    version="${tag#v}"
    printf '{"tag_name":"%s","assets":[' "${tag}"
    first=1
    for name in \
      "yeoul_${version}_darwin_amd64.tar.gz" \
      "yeoul_${version}_darwin_arm64.tar.gz" \
      "yeoul_${version}_linux_amd64.tar.gz" \
      "yeoul_${version}_linux_arm64.tar.gz" \
      "checksums_darwin-amd64.txt" \
      "checksums_darwin-arm64.txt" \
      "checksums_linux-amd64.txt" \
      "checksums_linux-arm64.txt"; do
      if [ "${first}" -eq 0 ]; then
        printf ','
      fi
      first=0
      printf '{"name":"%s","browser_download_url":"https://example.test/assets/%s"}' "${name}" "${name}"
    done
    printf ']}\n'
    ;;
  repos/*)
    printf '{"default_branch":"main"}\n'
    ;;
  *)
    printf 'unexpected gh api path: %s\n' "${path}" >&2
    exit 1
    ;;
esac
GH_STUB
chmod +x "${stub_dir}/gh"

cat > "${stub_dir}/curl" <<'CURL_STUB'
#!/bin/sh
set -eu
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    -H)
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
name="${url##*/}"
case "${name}" in
  checksums_*.txt)
    cp "${YEOUL_TEST_ASSETS}/${name}" "${out}"
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 22
    ;;
esac
CURL_STUB
chmod +x "${stub_dir}/curl"

assets_dir="${work_dir}/assets"
mkdir -p "${assets_dir}"
export YEOUL_TEST_ASSETS="${assets_dir}"

# The publisher resolves one checksum per platform for the tag being
# published, so the served checksum files are rewritten before each run.
write_tag_checksums() {
  version="$1"
  for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
    os_part="${platform%%-*}"
    arch_part="${platform##*-}"
    printf '%064d  %s\n' 1 "yeoul_${version}_${os_part}_${arch_part}.tar.gz" \
      > "${assets_dir}/checksums_${platform}.txt"
  done
}

# --- tap repository helpers -----------------------------------------------

seed_tap() {
  # seed_tap <bare-repo> <existing-version>
  bare="$1"
  version="$2"
  seed_dir="${work_dir}/seed-${RANDOM}-$$"

  git init --quiet --bare "${bare}"
  git --git-dir="${bare}" symbolic-ref HEAD refs/heads/main
  git init --quiet "${seed_dir}"
  git -C "${seed_dir}" config user.name "test"
  git -C "${seed_dir}" config user.email "test@example.invalid"
  mkdir -p "${seed_dir}/Formula"
  {
    printf 'class Yeoul < Formula\n'
    printf '  desc "Local-first temporal graph memory engine"\n'
    printf '  version "%s"\n' "${version}"
    printf 'end\n'
  } > "${seed_dir}/Formula/yeoul.rb"
  git -C "${seed_dir}" add Formula/yeoul.rb
  git -C "${seed_dir}" commit --quiet -m "seed ${version}"
  git -C "${seed_dir}" remote add origin "${bare}"
  git -C "${seed_dir}" push --quiet origin HEAD:main
}

tap_version() {
  git --git-dir="$1" show "main:Formula/yeoul.rb" \
    | sed -n 's/^[[:space:]]*version "\([^"]*\)".*/\1/p' | head -n1
}

tap_formula() {
  git --git-dir="$1" show "main:Formula/yeoul.rb"
}

tap_commit_count() {
  git --git-dir="$1" rev-list --count main
}

# run_publish <bare-repo> <tag> <log> <allow-rollback: 0|1>
run_publish() {
  bare="$1"
  tag="$2"
  log="$3"
  allow_rollback="$4"
  rc=0
  env \
    PATH="${stub_dir}:${PATH}" \
    HOMEBREW_TAP_GITHUB_TOKEN="test-token" \
    HOMEBREW_TAP_REPO="mrchypark/homebrew-tap" \
    SOURCE_REPO="mrchypark/yeoul" \
    HOMEBREW_ALLOW_VERSION_ROLLBACK="${allow_rollback}" \
    GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0="url.${bare}.insteadOf" \
    GIT_CONFIG_VALUE_0="https://x-access-token:test-token@github.com/mrchypark/homebrew-tap.git" \
    bash "${publisher_path}" "${tag}" > "${log}" 2>&1 || rc=$?
  printf '%s' "${rc}"
}

# --- REL-06: a late older release must not regress the formula ------------

section "REL-06 a late older release does not regress the formula"

tap_a="${work_dir}/tap-a.git"
seed_tap "${tap_a}" "0.5.5"
commits_before="$(tap_commit_count "${tap_a}")"
formula_before="$(tap_formula "${tap_a}")"

write_tag_checksums "0.5.4"
rc_a="$(run_publish "${tap_a}" "v0.5.4" "${work_dir}/tap-a.log" "0")"

assert_eq "older release is refused after a newer publication" "1" "${rc_a}"
assert_file_contains "refusal explains the version conflict" "${work_dir}/tap-a.log" \
  "already publishes yeoul 0.5.5; refusing to replace it with 0.5.4"
assert_file_contains "refusal documents the rollback override" "${work_dir}/tap-a.log" \
  "HOMEBREW_ALLOW_VERSION_ROLLBACK=1"
assert_eq "formula stays at the newer version" "0.5.5" "$(tap_version "${tap_a}")"
assert_eq "formula content is unchanged" "${formula_before}" "$(tap_formula "${tap_a}")"
assert_eq "no commit was published" "${commits_before}" "$(tap_commit_count "${tap_a}")"

section "REL-06 an intentional rollback remains available"

tap_b="${work_dir}/tap-b.git"
seed_tap "${tap_b}" "0.5.5"
write_tag_checksums "0.5.4"
rc_b="$(run_publish "${tap_b}" "v0.5.4" "${work_dir}/tap-b.log" "1")"

assert_eq "rollback override publishes the older version" "0" "${rc_b}"
assert_file_contains "rollback is reported" "${work_dir}/tap-b.log" \
  "publishing intentional rollback from yeoul 0.5.5 to 0.5.4"
assert_eq "formula records the rolled-back version" "0.5.4" "$(tap_version "${tap_b}")"

section "REL-06 forward publication still works"

tap_c="${work_dir}/tap-c.git"
seed_tap "${tap_c}" "0.5.4"
commits_before_c="$(tap_commit_count "${tap_c}")"
write_tag_checksums "0.5.5"
rc_c="$(run_publish "${tap_c}" "v0.5.5" "${work_dir}/tap-c.log" "0")"

assert_eq "newer release publishes over an older formula" "0" "${rc_c}"
assert_eq "formula records the newer version" "0.5.5" "$(tap_version "${tap_c}")"
assert_eq "exactly one new commit was published" "$((commits_before_c + 1))" "$(tap_commit_count "${tap_c}")"

commits_before_d="$(tap_commit_count "${tap_c}")"
rc_d="$(run_publish "${tap_c}" "v0.5.5" "${work_dir}/tap-d.log" "0")"

assert_eq "republishing the same version succeeds" "0" "${rc_d}"
assert_file_contains "republishing is reported as already up to date" "${work_dir}/tap-d.log" \
  "Homebrew formula already up to date"
assert_eq "republishing adds no commit" "${commits_before_d}" "$(tap_commit_count "${tap_c}")"

# --- workflow serialization ----------------------------------------------

section "REL-06 workflow serializes publication on the destination formula"

job_block_file="${work_dir}/publish-homebrew-job.txt"
awk '
  /^  [A-Za-z0-9_-]+:$/ {
    name = $1
    sub(/:$/, "", name)
    in_job = (name == "publish-homebrew")
    next
  }
  in_job { print }
' "${workflow_path}" > "${job_block_file}"

assert_eq "publish-homebrew job was located in the workflow" "1" \
  "$(grep -c '^    concurrency:$' "${job_block_file}" || true)"
assert_file_contains "concurrency group is keyed on the destination formula" "${job_block_file}" \
  "homebrew-formula-mrchypark-homebrew-tap"
assert_file_contains "concurrent publication is not cancelled" "${job_block_file}" \
  "cancel-in-progress: false"
assert_eq "concurrency group does not depend on the workflow ref" "0" \
  "$(grep -c 'group: release-' "${job_block_file}" || true)"
assert_file_contains "publication still depends on the published release" "${job_block_file}" \
  "publish-release"

# --- summary --------------------------------------------------------------

printf '\n'
if [[ "${failures}" -ne 0 ]]; then
  printf '%s of %s Homebrew publication checks failed\n' "${failures}" "${checks}" >&2
  exit 1
fi
printf 'Homebrew publication tests passed (%s checks)\n' "${checks}"
