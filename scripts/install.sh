#!/usr/bin/env bash

set -euo pipefail

repo="${YEOUL_REPO:-mrchypark/yeoul}"
version="${YEOUL_VERSION:-latest}"
install_root="${YEOUL_INSTALL_ROOT:-${HOME}/.local/share/yeoul}"
bin_dir="${YEOUL_BIN_DIR:-${HOME}/.local/bin}"

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
trap 'rm -rf "${temp_dir}"' EXIT

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
    echo "warning: sha256 verifier not found; skipping checksum verification" >&2
    return
  fi

  if [[ "${actual}" != "${expected}" ]]; then
    echo "checksum mismatch for ${archive_name}" >&2
    exit 1
  fi
}

verify_checksum

mkdir -p "${install_root}" "${bin_dir}"
target_dir="${install_root}/${tag}"
rm -rf "${target_dir}"

tar -xzf "${archive_path}" -C "${temp_dir}"
extracted_dir="$(find "${temp_dir}" -mindepth 1 -maxdepth 1 -type d -name "yeoul_*_${os}_${arch}" -print -quit)"

if [[ -z "${extracted_dir}" ]]; then
  echo "failed to locate extracted archive directory" >&2
  exit 1
fi

mv "${extracted_dir}" "${target_dir}"

cat > "${bin_dir}/yeoul" <<EOF
#!/usr/bin/env bash
exec "${target_dir}/bin/yeoul" "\$@"
EOF

cat > "${bin_dir}/yeould" <<EOF
#!/usr/bin/env bash
exec "${target_dir}/bin/yeould" "\$@"
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
