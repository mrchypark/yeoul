#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <target-os> <target-arch>" >&2
  exit 1
fi

target_os="$1"
target_arch="$2"
go mod download github.com/LadybugDB/go-ladybug >/dev/null
module_dir="$(go list -m -f '{{.Dir}}' github.com/LadybugDB/go-ladybug)"

if [[ -z "${module_dir}" ]]; then
  echo "failed to resolve go-ladybug module directory" >&2
  exit 1
fi

# The Go module cache is often read-only on CI runners.
chmod -R u+w "${module_dir}" 2>/dev/null || true

case "${target_os}/${target_arch}" in
  darwin/amd64)
    asset="liblbug-osx-x86_64.tar.gz"
    dest_dir="${module_dir}/lib/dynamic/darwin"
    runtime_files=("liblbug.dylib")
    ;;
  darwin/arm64)
    asset="liblbug-osx-arm64.tar.gz"
    dest_dir="${module_dir}/lib/dynamic/darwin"
    runtime_files=("liblbug.dylib")
    ;;
  linux/amd64)
    asset="liblbug-linux-x86_64.tar.gz"
    dest_dir="${module_dir}/lib/dynamic/linux-amd64"
    runtime_files=("liblbug.so")
    ;;
  linux/arm64)
    asset="liblbug-linux-aarch64.tar.gz"
    dest_dir="${module_dir}/lib/dynamic/linux-arm64"
    runtime_files=("liblbug.so")
    ;;
  windows/amd64)
    asset="liblbug-windows-x86_64.zip"
    dest_dir="${module_dir}/lib/dynamic/windows"
    runtime_files=("lbug_shared.dll" "lbug_shared.lib")
    ;;
  *)
    echo "unsupported Ladybug target: ${target_os}/${target_arch}" >&2
    exit 1
    ;;
esac

temp_dir="$(mktemp -d)"
trap 'rm -rf "${temp_dir}"' EXIT

archive_path="${temp_dir}/${asset}"
download_url="https://github.com/LadybugDB/ladybug/releases/latest/download/${asset}"

mkdir -p "${dest_dir}"

curl -fsSL "${download_url}" -o "${archive_path}"

case "${asset}" in
  *.tar.gz)
    tar -xzf "${archive_path}" -C "${temp_dir}"
    ;;
  *.zip)
    unzip -q "${archive_path}" -d "${temp_dir}"
    ;;
  *)
    echo "unsupported archive format: ${asset}" >&2
    exit 1
    ;;
esac

for runtime_file in "${runtime_files[@]}"; do
  found_file="$(find "${temp_dir}" -type f -name "${runtime_file}" -print -quit)"
  if [[ -z "${found_file}" ]]; then
    echo "missing runtime file ${runtime_file} in ${asset}" >&2
    exit 1
  fi
  cp "${found_file}" "${dest_dir}/${runtime_file}"
done

if [[ ! -f "${module_dir}/lbug.h" ]]; then
  header_file="$(find "${temp_dir}" -type f -name 'lbug.h' -print -quit || true)"
  if [[ -n "${header_file}" ]]; then
    cp "${header_file}" "${module_dir}/lbug.h"
  fi
fi

printf 'Installed Ladybug runtime for %s/%s into %s\n' "${target_os}" "${target_arch}" "${dest_dir}"
