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
module_version="$(go list -m -f '{{.Version}}' github.com/LadybugDB/go-ladybug)"

if [[ -z "${module_dir}" ]]; then
  echo "failed to resolve go-ladybug module directory" >&2
  exit 1
fi

# The Go module cache is often read-only on CI runners.
chmod -R u+w "${module_dir}" 2>/dev/null || true

case "${target_os}/${target_arch}" in
  darwin/amd64)
    assets=("liblbug-osx-x86_64.tar.gz" "liblbug-osx-universal.tar.gz")
    dest_dir="${module_dir}/lib/dynamic/darwin"
    cgo_dir="${module_dir}/lib"
    runtime_files=("liblbug.dylib" "liblbug.0.dylib")
    ;;
  darwin/arm64)
    assets=("liblbug-osx-arm64.tar.gz" "liblbug-osx-universal.tar.gz")
    dest_dir="${module_dir}/lib/dynamic/darwin"
    cgo_dir="${module_dir}/lib"
    runtime_files=("liblbug.dylib" "liblbug.0.dylib")
    ;;
  linux/amd64)
    assets=("liblbug-linux-x86_64.tar.gz")
    dest_dir="${module_dir}/lib/dynamic/linux-amd64"
    cgo_dir="${module_dir}/lib"
    runtime_files=("liblbug.so" "liblbug.so.0")
    ;;
  linux/arm64)
    assets=("liblbug-linux-aarch64.tar.gz")
    dest_dir="${module_dir}/lib/dynamic/linux-arm64"
    cgo_dir="${module_dir}/lib"
    runtime_files=("liblbug.so" "liblbug.so.0")
    ;;
  windows/amd64)
    assets=("liblbug-windows-x86_64.zip")
    dest_dir="${module_dir}/lib/dynamic/windows"
    cgo_dir="${module_dir}/lib"
    runtime_files=("lbug_shared.dll" "lbug_shared.lib")
    ;;
  *)
    echo "unsupported Ladybug target: ${target_os}/${target_arch}" >&2
    exit 1
    ;;
esac

temp_dir="$(mktemp -d)"
trap 'rm -rf "${temp_dir}"' EXIT

release_ref="${module_version}"
if [[ "${module_version}" =~ -[0-9]{14}-[0-9a-f]{12}$ ]]; then
  release_ref="latest"
fi

mkdir -p "${dest_dir}" "${cgo_dir}"

asset=""
archive_path=""
for candidate in "${assets[@]}"; do
  archive_path="${temp_dir}/${candidate}"
  if [[ "${release_ref}" == "latest" ]]; then
    download_url="https://github.com/LadybugDB/ladybug/releases/latest/download/${candidate}"
  else
    download_url="https://github.com/LadybugDB/ladybug/releases/download/${release_ref}/${candidate}"
  fi
  if curl -fsSL "${download_url}" -o "${archive_path}" 2>/dev/null; then
    asset="${candidate}"
    break
  fi
done

if [[ -z "${asset}" ]]; then
  echo "failed to download Ladybug runtime for ${target_os}/${target_arch} from ${release_ref}" >&2
  exit 1
fi

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
  found_file="$(find "${temp_dir}" \( -type f -o -type l \) -name "${runtime_file}" -print -quit || true)"
  if [[ -z "${found_file}" ]]; then
    case "${runtime_file}" in
      liblbug.0.dylib)
        found_file="$(find "${temp_dir}" \( -type f -o -type l \) -name 'liblbug.dylib' -print -quit || true)"
        ;;
      liblbug.so.0)
        found_file="$(find "${temp_dir}" \( -type f -o -type l \) -name 'liblbug.so' -print -quit || true)"
        ;;
    esac
  fi
  if [[ -z "${found_file}" ]]; then
    echo "missing runtime file ${runtime_file} in ${asset}" >&2
    exit 1
  fi
  cp -L "${found_file}" "${dest_dir}/${runtime_file}"
  cp -L "${found_file}" "${cgo_dir}/${runtime_file}"
done

if [[ ! -f "${cgo_dir}/lbug.h" ]]; then
  header_file="$(find "${temp_dir}" \( -type f -o -type l \) -name 'lbug.h' -print -quit || true)"
  if [[ -n "${header_file}" ]]; then
    cp -L "${header_file}" "${cgo_dir}/lbug.h"
  fi
fi

printf 'Installed Ladybug runtime for %s/%s into %s\n' "${target_os}" "${target_arch}" "${dest_dir}"
