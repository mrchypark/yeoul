#!/bin/sh
# shellcheck disable=SC2016
#
# Regression tests for scripts/install.sh release compatibility and install-path
# handling. The installer is exercised with real command invocations against
# synthetic release fixtures served by a stub curl, so the checks cover the
# actual script behaviour instead of a reimplementation of it.
#
# Covered findings:
#   REL-02  historical releases without the migration helper still install
#   REL-03  an explicit version wins over the latest release metadata
#   REL-04  a missing checksum verifier fails before installation state changes
#   REL-05  install roots and wrapper paths survive unrelated working directories
#   SEC-01  no workstation audit artifact is tracked in the repository
#
# Requires: sh, bash (the installer is a bash script), tar, awk, sed, grep. GNU
# tar delegates compression to a separate gzip child, so gzip must be present
# on the host PATH as well as inside every installer sandbox below.

set -eu

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd "${script_dir}/../.." && pwd)
installer_path="${repo_root}/scripts/install.sh"

if [ ! -f "${installer_path}" ]; then
  echo "installer not found at ${installer_path}" >&2
  exit 1
fi

bash_path=$(command -v bash || true)
if [ -z "${bash_path}" ]; then
  echo "missing required command: bash" >&2
  exit 1
fi

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "unsupported test host: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported test architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

host_sha256_tool=""
if command -v sha256sum >/dev/null 2>&1; then
  host_sha256_tool="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  host_sha256_tool="shasum"
fi
if [ -z "${host_sha256_tool}" ]; then
  echo "missing required command: sha256sum or shasum (needed to build fixtures)" >&2
  exit 1
fi

work_dir=$(mktemp -d)
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
  if [ "$2" = "$3" ]; then
    ok "$1"
  else
    fail "$1" "$2" "$3"
  fi
}

assert_present() {
  checks=$((checks + 1))
  if [ -e "$2" ] || [ -L "$2" ]; then
    ok "$1"
  else
    fail "$1" "present" "absent: $2"
  fi
}

assert_absent() {
  checks=$((checks + 1))
  if [ -e "$2" ] || [ -L "$2" ]; then
    fail "$1" "absent" "present: $2"
  else
    ok "$1"
  fi
}

assert_file_contains() {
  checks=$((checks + 1))
  if [ -f "$2" ] && grep -F -e "$3" "$2" >/dev/null 2>&1; then
    ok "$1"
  else
    fail "$1" "contains: $3" "not found in $2"
  fi
}

# contains <file> <literal> -> yes|no
contains() {
  if [ -f "$1" ] && grep -F -q -e "$2" "$1" 2>/dev/null; then
    printf 'yes'
  else
    printf 'no'
  fi
}

sha256_of() {
  case "${host_sha256_tool}" in
    sha256sum) sha256sum "$1" | awk '{print $1}' ;;
    shasum) shasum -a 256 "$1" | awk '{print $1}' ;;
  esac
}

# --- fixtures -------------------------------------------------------------

fixtures="${work_dir}/fixtures"

# create_release_fixture <tag> <with-helper>
create_release_fixture() {
  tag="$1"
  with_helper="$2"
  version="${tag#v}"
  archive_name="yeoul_${version}_${os}_${arch}.tar.gz"
  fixture_dir="${fixtures}/${tag}"
  tree_name="yeoul_${version}_${os}_${arch}"
  tree="${fixture_dir}/${tree_name}"

  mkdir -p "${tree}/bin" "${tree}/lib"
  printf '#!/bin/sh\necho yeoul %s\n' "${tag}" > "${tree}/bin/yeoul"
  printf '#!/bin/sh\necho yeould %s\n' "${tag}" > "${tree}/bin/yeould"
  chmod +x "${tree}/bin/yeoul" "${tree}/bin/yeould"
  printf 'runtime for %s\n' "${tag}" > "${tree}/lib/libladybug.txt"

  if [ "${with_helper}" = "yes" ]; then
    mkdir -p "${tree}/libexec/ladybug-v0131"
    printf '#!/bin/sh\necho migrate %s\n' "${tag}" > "${tree}/libexec/ladybug-v0131/yeoul-migrate-v0131"
    chmod +x "${tree}/libexec/ladybug-v0131/yeoul-migrate-v0131"
  fi

  tar -czf "${fixture_dir}/${archive_name}" -C "${fixture_dir}" "${tree_name}"
  printf '%s  %s\n' "$(sha256_of "${fixture_dir}/${archive_name}")" "${archive_name}" \
    > "${fixture_dir}/checksums_${os}-${arch}.txt"
}

# v0.1.0 and v0.5.0 predate the helper; v0.5.1 is the first release that ships
# it. v0.5.6 is the release served as "latest" and is intentionally incomplete,
# so a rejected archive is distinguishable from an accepted one.
create_release_fixture "v0.1.0" "no"
create_release_fixture "v0.5.0" "no"
create_release_fixture "v0.5.5" "yes"
create_release_fixture "v0.5.6" "no"

# --- PATH sandboxes -------------------------------------------------------

# The installer runs with a PATH that contains only the tools it is allowed to
# see, so the "verifier is absent" case cannot be satisfied by the host PATH.
# gzip is listed because GNU tar execs it as a child when extracting -z
# archives; bsdtar links libz directly and does not need it.
sandbox_tools="awk bash cat chmod cp dirname env expr find grep gzip head ln mkdir mktemp mv rm rmdir sed sort tar touch uname"

link_host_tools() {
  sandbox="$1"
  for tool in ${sandbox_tools}; do
    target=$(command -v "${tool}" 2>/dev/null || true)
    case "${target}" in
      /*) ln -s "${target}" "${sandbox}/${tool}" ;;
    esac
  done
}

sandbox_verified="${work_dir}/path-verified"
sandbox_unverified="${work_dir}/path-unverified"
mkdir -p "${sandbox_verified}" "${sandbox_unverified}"
link_host_tools "${sandbox_verified}"
link_host_tools "${sandbox_unverified}"
ln -s "$(command -v "${host_sha256_tool}")" "${sandbox_verified}/${host_sha256_tool}"

# curl stub: logs every request and serves the release metadata plus fixtures.
for sandbox in "${sandbox_verified}" "${sandbox_unverified}"; do
  cat > "${sandbox}/curl" <<'CURL_STUB'
#!/bin/sh
set -eu
log="${YEOUL_TEST_CURL_LOG:?}"
fixtures="${YEOUL_TEST_FIXTURES:?}"
printf '%s\n' "$*" >> "${log}"

output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
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

case "${url}" in
  */releases/latest)
    printf '%s\n' '{"tag_name": "v0.5.6"}'
    ;;
  */releases/download/*/*)
    rest="${url#*/releases/download/}"
    tag="${rest%%/*}"
    name="${url##*/}"
    cp "${fixtures}/${tag}/${name}" "${output}"
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 22
    ;;
esac
CURL_STUB
  chmod +x "${sandbox}/curl"
done

export YEOUL_TEST_FIXTURES="${fixtures}"

install_with() {
  sandbox="$1"
  run_cwd="$2"
  log="$3"
  requested_version="$4"
  shift 4
  rc=0
  if [ -n "${requested_version}" ]; then
    ( cd "${run_cwd}" && PATH="${sandbox}" YEOUL_VERSION="${requested_version}" \
      "${bash_path}" "${installer_path}" "$@" ) > "${log}" 2>&1 || rc=$?
  else
    ( cd "${run_cwd}" && PATH="${sandbox}" \
      "${bash_path}" "${installer_path}" "$@" ) > "${log}" 2>&1 || rc=$?
  fi
  printf '%s' "${rc}"
}

# --- REL-04: missing checksum verifier ------------------------------------

section "REL-04 missing checksum verifier stops before changing state"

rel04_root="${work_dir}/rel04-root"
rel04_bin="${work_dir}/rel04-bin"
mkdir -p "${rel04_root}/v0.5.4/bin" "${rel04_bin}"
printf '#!/bin/sh\necho yeoul v0.5.4\n' > "${rel04_root}/v0.5.4/bin/yeoul"
chmod +x "${rel04_root}/v0.5.4/bin/yeoul"
printf '#!/bin/sh\necho wrapper v0.5.4\n' > "${rel04_bin}/yeoul"
chmod +x "${rel04_bin}/yeoul"

rel04_root_before=$(find "${rel04_root}" -mindepth 1 | sort)
rel04_wrapper_before=$(cat "${rel04_bin}/yeoul")

YEOUL_TEST_CURL_LOG="${work_dir}/rel04.log.curl"
export YEOUL_TEST_CURL_LOG
rel04_rc=$(install_with "${sandbox_unverified}" "${work_dir}" "${work_dir}/rel04.log" "v0.5.5" \
  --install-root "${rel04_root}" --bin-dir "${rel04_bin}")

assert_eq "installer fails without sha256sum or shasum" "1" "${rel04_rc}"
assert_file_contains "installer names the missing verifier" "${work_dir}/rel04.log" \
  "missing required command: sha256sum or shasum"
assert_eq "existing installation is preserved" "${rel04_root_before}" "$(find "${rel04_root}" -mindepth 1 | sort)"
assert_eq "existing wrapper is preserved" "${rel04_wrapper_before}" "$(cat "${rel04_bin}/yeoul")"
assert_absent "no new version directory was created" "${rel04_root}/v0.5.5"
assert_eq "existing wrapper still resolves the old version" "wrapper v0.5.4" "$("${rel04_bin}/yeoul")"

# --- REL-02: historical releases and layout validation --------------------

section "REL-02 release layout is validated against the selected tag"

rel02_root="${work_dir}/rel02-root"
rel02_bin="${work_dir}/rel02-bin"

YEOUL_TEST_CURL_LOG="${work_dir}/rel02.log.curl"
export YEOUL_TEST_CURL_LOG
rel02_old_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel02-old.log" "v0.1.0" \
  --install-root "${rel02_root}" --bin-dir "${rel02_bin}")

assert_eq "v0.1.0 installs without a migration helper" "0" "${rel02_old_rc}"
assert_present "v0.1.0 binaries are installed" "${rel02_root}/v0.1.0/bin/yeoul"
assert_eq "v0.1.0 wrapper resolves the installed binary" "yeoul v0.1.0" "$("${rel02_bin}/yeoul")"

rel02_prev_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel02-prev.log" "v0.5.0" \
  --install-root "${rel02_root}" --bin-dir "${rel02_bin}")
assert_eq "v0.5.0 installs without a migration helper" "0" "${rel02_prev_rc}"

rel02_cur_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel02-cur.log" "v0.5.5" \
  --install-root "${rel02_root}" --bin-dir "${rel02_bin}")
assert_eq "v0.5.5 installs with its migration helper" "0" "${rel02_cur_rc}"
assert_present "v0.5.5 migration helper is installed" \
  "${rel02_root}/v0.5.5/libexec/ladybug-v0131/yeoul-migrate-v0131"

rel02_incomplete_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel02-incomplete.log" "v0.5.6" \
  --install-root "${rel02_root}" --bin-dir "${rel02_bin}")
assert_eq "v0.5.6 without the helper is rejected" "1" "${rel02_incomplete_rc}"
assert_file_contains "rejection names the missing helper" "${work_dir}/rel02-incomplete.log" \
  "missing the version-pinned Ladybug migration helper"
assert_absent "rejected archive leaves no v0.5.6 directory" "${rel02_root}/v0.5.6"

YEOUL_TEST_CURL_LOG="${work_dir}/rel02-latest.log.curl"
export YEOUL_TEST_CURL_LOG
rel02_latest_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel02-latest.log" "" \
  --install-root "${rel02_root}" --bin-dir "${rel02_bin}")
assert_eq "incomplete latest release is rejected too" "1" "${rel02_latest_rc}"
assert_file_contains "latest rejection names the missing helper" "${work_dir}/rel02-latest.log" \
  "missing the version-pinned Ladybug migration helper"

# --- REL-03: explicit version wins over latest metadata -------------------

section "REL-03 explicit version wins over latest release metadata"

rel03_root="${work_dir}/rel03-root"
rel03_bin="${work_dir}/rel03-bin"
YEOUL_TEST_CURL_LOG="${work_dir}/rel03.log.curl"
export YEOUL_TEST_CURL_LOG
rel03_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel03.log" "v0.5.5" \
  --install-root "${rel03_root}" --bin-dir "${rel03_bin}")

assert_eq "explicit v0.5.5 installs" "0" "${rel03_rc}"
assert_present "requested version is installed" "${rel03_root}/v0.5.5/bin/yeoul"
assert_absent "latest metadata did not install v0.5.6" "${rel03_root}/v0.5.6"
assert_eq "requested archive was downloaded" "yes" "$(contains "${work_dir}/rel03.log.curl" "releases/download/v0.5.5/yeoul_")"
assert_eq "latest archive was not downloaded" "no" "$(contains "${work_dir}/rel03.log.curl" "releases/download/v0.5.6/")"
assert_eq "latest release metadata was never queried" "no" "$(contains "${work_dir}/rel03.log.curl" "releases/latest")"

# --- REL-05: install roots and wrapper paths ------------------------------

section "REL-05 install roots survive unrelated working directories"

rel05_root="${work_dir}/rel05-root"
rel05_bin="${work_dir}/rel05-bin"
YEOUL_TEST_CURL_LOG="${work_dir}/rel05-abs.log.curl"
export YEOUL_TEST_CURL_LOG
rel05_abs_rc=$(install_with "${sandbox_verified}" "/" "${work_dir}/rel05-abs.log" "v0.5.5" \
  --install-root "${rel05_root}" --bin-dir "${rel05_bin}")
assert_eq "absolute root installs from an unrelated directory" "0" "${rel05_abs_rc}"
assert_file_contains "wrapper stores the absolute binary path" "${rel05_bin}/yeoul" \
  "exec '${rel05_root}/v0.5.5/bin/yeoul'"
assert_eq "absolute-root wrapper resolves from /" "yeoul v0.5.5" "$(cd / && "${rel05_bin}/yeoul")"

rel05_rel_root="rel05-relative-root"
rel05_rel_bin="rel05-relative-bin"
YEOUL_TEST_CURL_LOG="${work_dir}/rel05-rel.log.curl"
export YEOUL_TEST_CURL_LOG
rel05_rel_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel05-rel.log" "v0.5.5" \
  --install-root "${rel05_rel_root}" --bin-dir "${rel05_rel_bin}")
assert_eq "relative root installs" "0" "${rel05_rel_rc}"
assert_present "relative root resolves against the invocation directory" \
  "${work_dir}/${rel05_rel_root}/v0.5.5/bin/yeoul"
assert_file_contains "relative-root wrapper stores an absolute path" "${work_dir}/${rel05_rel_bin}/yeoul" \
  "exec '${work_dir}/${rel05_rel_root}/v0.5.5/bin/yeoul'"
assert_eq "relative-root wrapper resolves from /" "yeoul v0.5.5" \
  "$(cd / && "${work_dir}/${rel05_rel_bin}/yeoul")"

rel05_literal_root='rel05-$cash-root'
rel05_literal_bin='rel05-$cash-bin'
YEOUL_TEST_CURL_LOG="${work_dir}/rel05-literal.log.curl"
export YEOUL_TEST_CURL_LOG
rel05_literal_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel05-literal.log" "v0.5.5" \
  --install-root "${rel05_literal_root}" --bin-dir "${rel05_literal_bin}")
assert_eq "root with a literal dollar installs" "0" "${rel05_literal_rc}"
assert_file_contains "literal dollar path is serialized as data" "${work_dir}/${rel05_literal_bin}/yeoul" \
  "exec '${work_dir}/${rel05_literal_root}/v0.5.5/bin/yeoul'"
assert_eq "literal dollar wrapper resolves from /" "yeoul v0.5.5" \
  "$(cd / && "${work_dir}/${rel05_literal_bin}/yeoul")"

rel05_space_root="${work_dir}/rel05 space root"
rel05_space_bin="${work_dir}/rel05 space bin"
YEOUL_TEST_CURL_LOG="${work_dir}/rel05-space.log.curl"
export YEOUL_TEST_CURL_LOG
rel05_space_rc=$(install_with "${sandbox_verified}" "${work_dir}" "${work_dir}/rel05-space.log" "v0.5.5" \
  --install-root "${rel05_space_root}" --bin-dir "${rel05_space_bin}")
assert_eq "root with spaces installs" "0" "${rel05_space_rc}"
assert_eq "space-root wrapper resolves from /" "yeoul v0.5.5" "$(cd / && "${rel05_space_bin}/yeoul")"

# --- README examples ------------------------------------------------------

section "README version-pinned examples select the binary version"

readme="${repo_root}/README.md"
unpinned_examples=$(awk '
  /^```/ {
    if (in_block) {
      if (block ~ "releases/download/v[0-9]" && block !~ "YEOUL_VERSION") {
        count++
      }
      in_block = 0
      block = ""
    } else {
      in_block = 1
      block = ""
    }
    next
  }
  in_block { block = block $0 "\n" }
  END {
    if (in_block && block ~ "releases/download/v[0-9]" && block !~ "YEOUL_VERSION") {
      count++
    }
    print count + 0
  }
' "${readme}")

assert_eq "every version-pinned install example pins YEOUL_VERSION" "0" "${unpinned_examples}"
assert_file_contains "README documents the helper compatibility policy" "${readme}" \
  'Releases from `v0.5.1` on must ship the version-pinned Ladybug migration helper'

# --- SEC-01: workstation inventory ----------------------------------------

section "SEC-01 workstation audit artifacts stay out of the repository"

assert_absent "info.md is not in the working tree" "${repo_root}/info.md"
assert_eq "info.md is not tracked" "" "$(git -C "${repo_root}" ls-files -- info.md 2>/dev/null || true)"
tracked_audit=$(git -C "${repo_root}" grep -l -E '^(- last_full_scan:|## /Users/[A-Za-z0-9._-]+/)' -- . 2>/dev/null || true)
assert_eq "no tracked file carries a workstation audit report" "" "${tracked_audit}"

# --- summary --------------------------------------------------------------

printf '\n'
if [ "${failures}" -ne 0 ]; then
  printf '%s of %s release installer checks failed\n' "${failures}" "${checks}" >&2
  exit 1
fi
printf 'release installer tests passed (%s checks)\n' "${checks}"
