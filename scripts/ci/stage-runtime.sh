#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <target-os> <target-arch> <binary-path>" >&2
  exit 1
fi

target_os="$1"
target_arch="$2"
binary_path="$3"
module_dir="$(go list -f '{{.Dir}}' -m github.com/LadybugDB/go-ladybug)"
runtime_root="dist/runtime/${target_os}_${target_arch}/lib"

case "${target_os}/${target_arch}" in
  linux/amd64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib/dynamic/linux-amd64"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so.0"
    if command -v patchelf >/dev/null 2>&1 && ldd "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      patchelf --set-rpath '$ORIGIN/../lib' "${binary_path}"
    fi
    ;;
  linux/arm64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib/dynamic/linux-arm64"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so.0"
    if command -v patchelf >/dev/null 2>&1 && ldd "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      patchelf --set-rpath '$ORIGIN/../lib' "${binary_path}"
    fi
    ;;
  darwin/amd64 | darwin/arm64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib/dynamic/darwin"
    cp "${src_dir}/liblbug.dylib" "${runtime_root}/liblbug.dylib"
    cp "${src_dir}/liblbug.dylib" "${runtime_root}/liblbug.0.dylib"
    if otool -L "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      install_name_tool -delete_rpath "${src_dir}" "${binary_path}" || true
      install_name_tool -add_rpath "@loader_path/../lib" "${binary_path}"
    fi
    ;;
  windows/amd64)
    runtime_root="dist/runtime/${target_os}_${target_arch}/bin"
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib/dynamic/windows"
    cp "${src_dir}/lbug_shared.dll" "${runtime_root}/lbug_shared.dll"
    ;;
  *)
    echo "unsupported runtime target: ${target_os}/${target_arch}" >&2
    exit 1
    ;;
esac
