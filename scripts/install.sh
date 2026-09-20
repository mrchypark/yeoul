#!/usr/bin/env bash

set -euo pipefail

repo="${YEOUL_REPO:-mrchypark/yeoul}"
version="${YEOUL_VERSION:-latest}"
install_root="${YEOUL_INSTALL_ROOT:-${HOME}/.local/share/yeoul}"
bin_dir="${YEOUL_BIN_DIR:-${HOME}/.local/bin}"

# The version-pinned Ladybug migration helper first shipped inside the v0.5.1
# archives. Older tags only carry the binaries, so requiring the helper for
# them would reject supported historical installs.
migration_helper_min_version="v0.5.1"

absolute_path() {
  # Resolve "$1" against the current directory so generated wrappers keep
  # working when they are invoked from an unrelated working directory.
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s\n' "${PWD}/$1" ;;
  esac
}

shell_quote() {
  # Serialize "$1" as a single-quoted shell word so literal expansion
  # characters in install paths stay data inside the generated wrappers.
  printf "'%s'" "${1//\'/\'\\\'\'}"
}

usage() {
  cat <<'EOF'
Usage:
  install.sh [--version TAG] [--install-root DIR] [--bin-dir DIR]

Environment:
  YEOUL_VERSION       Release tag to install, for example v0.1.0. Defaults to latest.
  YEOUL_INSTALL_ROOT  Install root. Defaults to ~/.local/share/yeoul.
  YEOUL_BIN_DIR       Wrapper script directory. Defaults to ~/.local/bin.
  YEOUL_REPO          GitHub repository in OWNER/REPO form. Defaults to mrchypark/yeoul.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="$2"
      shift 2
      ;;
    --install-root)
      install_root="$2"
      shift 2
      ;;
    --bin-dir)
      bin_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

install_root="$(absolute_path "${install_root}")"
bin_dir="$(absolute_path "${bin_dir}")"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd tar

os="$(uname -s)"
arch="$(uname -m)"

case "${os}" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "unsupported operating system: ${os}" >&2
    exit 1
    ;;
esac

case "${arch}" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported architecture: ${arch}" >&2
    exit 1
    ;;
esac

resolve_tag() {
  if [[ "${version}" != "latest" ]]; then
    if [[ "${version}" == v* ]]; then
      printf '%s\n' "${version}"
    else
      printf 'v%s\n' "${version}"
    fi
    return
  fi

  curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" | \
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1
}

# version_at_least <tag> <minimum-tag>
# Compares dotted release tags like v0.5.1 without relying on "sort -V", which
# BusyBox sort does not implement.
version_at_least() {
  awk -v have="$1" -v want="$2" 'BEGIN {
    sub(/^v/, "", have)
    sub(/^v/, "", want)
    n = split(have, h, ".")
    m = split(want, w, ".")
    count = (n > m) ? n : m
    for (i = 1; i <= count; i++) {
      a = (i <= n) ? h[i] + 0 : 0
      b = (i <= m) ? w[i] + 0 : 0
      if (a != b) {
        if (a > b) { exit 0 } else { exit 1 }
      }
    }
    exit 0
  }'
}

tag="$(resolve_tag)"
if [[ -z "${tag}" ]]; then
  echo "failed to resolve release tag" >&2
  exit 1
fi

asset_version="${tag#v}"
archive_name="yeoul_${asset_version}_${os}_${arch}.tar.gz"
checksum_name="checksums_${os}-${arch}.txt"
base_url="https://github.com/${repo}/releases/download/${tag}"

temp_dir="$(mktemp -d)"
staging_dir=""
cleanup() {
  rm -rf "${temp_dir}"
  if [[ -n "${staging_dir}" ]]; then
    rm -rf "${staging_dir}"
  fi
}
trap cleanup EXIT

archive_path="${temp_dir}/${archive_name}"
checksum_path="${temp_dir}/${checksum_name}"

curl -fsSL "${base_url}/${archive_name}" -o "${archive_path}"
curl -fsSL "${base_url}/${checksum_name}" -o "${checksum_path}"

verify_checksum() {
  local expected actual
  expected="$(grep " ${archive_name}\$" "${checksum_path}" | awk '{print $1}')"
  if [[ -z "${expected}" ]]; then
    echo "missing checksum entry for ${archive_name}" >&2
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${archive_path}" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
  else
    echo "missing required command: sha256sum or shasum; cannot verify ${archive_name}" >&2
    exit 1
  fi

  if [[ "${actual}" != "${expected}" ]]; then
    echo "checksum mismatch for ${archive_name}" >&2
    exit 1
  fi
}

verify_checksum

mkdir -p "${install_root}" "${bin_dir}"
target_dir="${install_root}/${tag}"
staging_dir="${install_root}/.${tag}.staging-${RANDOM:-0}-$$"
backup_dir="${install_root}/.${tag}.previous-${RANDOM:-0}-$$"
rm -rf "${staging_dir}" "${backup_dir}"

tar -xzf "${archive_path}" -C "${temp_dir}"
extracted_dir="$(find "${temp_dir}" -mindepth 1 -maxdepth 1 -type d -name "yeoul_*_${os}_${arch}" -print -quit)"

if [[ -z "${extracted_dir}" ]]; then
  echo "failed to locate extracted archive directory" >&2
  exit 1
fi

mv "${extracted_dir}" "${staging_dir}"

for executable in yeoul yeould; do
  if [[ ! -x "${staging_dir}/bin/${executable}" ]]; then
    echo "archive is missing executable bin/${executable}" >&2
    exit 1
  fi
done

if version_at_least "${tag}" "${migration_helper_min_version}"; then
  if [[ ! -x "${staging_dir}/libexec/ladybug-v0131/yeoul-migrate-v0131" ]]; then
    echo "archive is missing the version-pinned Ladybug migration helper" >&2
    exit 1
  fi
fi

if [[ -e "${target_dir}" || -L "${target_dir}" ]]; then
  mv "${target_dir}" "${backup_dir}"
fi
if ! mv "${staging_dir}" "${target_dir}"; then
  if [[ -e "${backup_dir}" || -L "${backup_dir}" ]]; then
    mv "${backup_dir}" "${target_dir}"
  fi
  echo "failed to replace ${target_dir}" >&2
  exit 1
fi
staging_dir=""
rm -rf "${backup_dir}"

cat > "${bin_dir}/yeoul" <<EOF
#!/usr/bin/env bash
exec $(shell_quote "${target_dir}/bin/yeoul") "\$@"
EOF

cat > "${bin_dir}/yeould" <<EOF
#!/usr/bin/env bash
exec $(shell_quote "${target_dir}/bin/yeould") "\$@"
EOF

chmod +x "${bin_dir}/yeoul" "${bin_dir}/yeould"

printf 'Installed Yeoul %s to %s\n' "${tag}" "${target_dir}"
printf 'Command wrappers written to %s\n' "${bin_dir}"

case ":$PATH:" in
  *":${bin_dir}:"*) ;;
  *)
    printf 'Add %s to PATH to use yeoul from new shells.\n' "${bin_dir}"
    ;;
esac
