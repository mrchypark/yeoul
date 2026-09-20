#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd "$(dirname "$0")" && pwd)"

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <target-os> <target-arch> <binary-path>" >&2
  exit 1
fi

target_os="$1"
target_arch="$2"
binary_path="$3"
rax_version="${YEOUL_RAX_VERSION:-v0.4.4}"
legacy_helper_lock_dir=""

cleanup_legacy_helper_lock() {
  if [[ -n "${legacy_helper_lock_dir}" ]]; then
    rmdir "${legacy_helper_lock_dir}" 2>/dev/null || true
  fi
}

trap cleanup_legacy_helper_lock EXIT

go mod download github.com/LadybugDB/go-ladybug >/dev/null
module_dir="$(go list -m -f '{{.Dir}}' github.com/LadybugDB/go-ladybug)"

if [[ -z "${module_dir}" ]]; then
  echo "failed to resolve go-ladybug module directory" >&2
  exit 1
fi
runtime_root="dist/runtime/${target_os}_${target_arch}/lib"

stage_legacy_migration_helper() {
  local complete_marker helper_dir helper_name legacy_module_dir legacy_src_dir
  helper_dir="dist/runtime/${target_os}_${target_arch}/libexec/ladybug-v0131"
  complete_marker="${helper_dir}.complete"
  helper_name="yeoul-migrate-v0131"
  if [[ "${target_os}" == "windows" ]]; then
    helper_name="${helper_name}.exe"
  fi
  if [[ -f "${complete_marker}" ]]; then
    return
  fi

  mkdir -p "$(dirname "${helper_dir}")"
  legacy_helper_lock_dir="${helper_dir}.lock"
  until mkdir "${legacy_helper_lock_dir}" 2>/dev/null; do
    if [[ -f "${complete_marker}" ]]; then
      legacy_helper_lock_dir=""
      return
    fi
    sleep 0.1
  done
  if [[ -f "${complete_marker}" ]]; then
    rmdir "${legacy_helper_lock_dir}"
    legacy_helper_lock_dir=""
    return
  fi

  ./scripts/ci/setup-ladybug.sh "${target_os}" "${target_arch}" go.legacy-v0131.mod
  legacy_module_dir="$(GOWORK=off go list -modfile=go.legacy-v0131.mod -m -f '{{.Dir}}' github.com/LadybugDB/go-ladybug)"
  legacy_src_dir="${legacy_module_dir}/lib"
  mkdir -p "${helper_dir}"
  GOWORK=off GOOS="${target_os}" GOARCH="${target_arch}" CGO_ENABLED=1 \
    go build -modfile=go.legacy-v0131.mod -trimpath \
      -ldflags='-s -w -X github.com/mrchypark/yeoul/pkg/yeoul.legacyMigrationReaderVersion=v0.13.1' \
      -o "${helper_dir}/${helper_name}" ./cmd/yeoul

  case "${target_os}/${target_arch}" in
    linux/*)
      cp "${legacy_src_dir}/liblbug.so" "${helper_dir}/liblbug.so"
      cp "${legacy_src_dir}/liblbug.so.0" "${helper_dir}/liblbug.so.0"
      if command -v patchelf >/dev/null 2>&1; then
        patchelf --set-rpath '$ORIGIN' "${helper_dir}/${helper_name}"
      fi
      ;;
    darwin/*)
      cp "${legacy_src_dir}/liblbug.dylib" "${helper_dir}/liblbug.dylib"
      cp "${legacy_src_dir}/liblbug.0.dylib" "${helper_dir}/liblbug.0.dylib"
      install_name_tool -delete_rpath "${legacy_src_dir}" "${helper_dir}/${helper_name}" 2>/dev/null || true
      install_name_tool -delete_rpath "${legacy_src_dir}/dynamic/darwin" "${helper_dir}/${helper_name}" 2>/dev/null || true
      install_name_tool -add_rpath "@loader_path" "${helper_dir}/${helper_name}"
      ;;
    windows/*)
      cp "${legacy_src_dir}/lbug_shared.dll" "${helper_dir}/lbug_shared.dll"
      ;;
  esac
  touch "${complete_marker}"
  rmdir "${legacy_helper_lock_dir}"
  legacy_helper_lock_dir=""
}

stage_rax_runtime() {
  local lib_name runtime_dest asset platform tmp_dir archive src_dir built_lib
  lib_name="librax_ffi.so"
  runtime_dest="dist/runtime/${target_os}_${target_arch}/lib"
  if [[ "${target_os}" == "windows" ]]; then
    lib_name="rax_ffi.dll"
    runtime_dest="dist/runtime/${target_os}_${target_arch}/bin"
  elif [[ "${target_os}" == "darwin" ]]; then
    lib_name="librax_ffi.dylib"
  fi

  mkdir -p "${runtime_dest}"
  if [[ -f "${runtime_dest}/${lib_name}" ]]; then
    return
  fi

  if [[ -n "${YEOUL_RAX_LIB:-}" ]]; then
    cp "${YEOUL_RAX_LIB}" "${runtime_dest}/${lib_name}"
    return
  fi

  asset=""
  case "${target_os}/${target_arch}" in
    linux/amd64) asset="rax-ffi-${rax_version}-linux-x86_64.tar.gz" ;;
    darwin/amd64) asset="rax-ffi-${rax_version}-macos-x86_64.tar.gz" ;;
    darwin/arm64) asset="rax-ffi-${rax_version}-macos-arm64.tar.gz" ;;
    windows/amd64) asset="rax-ffi-${rax_version}-windows-x86_64.zip" ;;
  esac

  if [[ -n "${asset}" ]]; then
    tmp_dir="$(mktemp -d)"
    archive="${tmp_dir}/${asset}"
    curl -fsSL "https://github.com/mrchypark/rax/releases/download/${rax_version}/${asset}" -o "${archive}"
    "${script_dir}/verify-runtime-digest.sh" "${rax_version}" "${asset}" "${archive}"
    if [[ "${asset}" == *.zip ]]; then
      unzip -q "${archive}" -d "${tmp_dir}"
    else
      tar -xzf "${archive}" -C "${tmp_dir}"
    fi
    built_lib="$(find "${tmp_dir}" -type f -name "${lib_name}" -print -quit)"
    if [[ -z "${built_lib}" ]]; then
      echo "failed to locate ${lib_name} in ${asset}" >&2
      exit 1
    fi
    cp "${built_lib}" "${runtime_dest}/${lib_name}"
    rm -rf "${tmp_dir}"
    return
  fi

  if [[ -n "${YEOUL_RAX_SOURCE_DIR:-}" ]]; then
    src_dir="${YEOUL_RAX_SOURCE_DIR}"
    tmp_dir=""
  else
    if ! command -v cargo >/dev/null 2>&1; then
      echo "cargo is required to build bundled rax runtime" >&2
      exit 1
    fi
    if ! command -v git >/dev/null 2>&1; then
      echo "git is required to fetch bundled rax runtime source" >&2
      exit 1
    fi
    tmp_dir="$(mktemp -d)"
    git clone --depth 1 --branch "${rax_version}" https://github.com/mrchypark/rax.git "${tmp_dir}/rax" >/dev/null
    src_dir="${tmp_dir}/rax"
  fi

  (
    cd "${src_dir}"
    cargo build -p rax-ffi --release --locked
  )

  built_lib="${src_dir}/target/release/${lib_name}"
  if [[ ! -f "${built_lib}" ]]; then
    echo "failed to build rax runtime at ${built_lib}" >&2
    exit 1
  fi
  cp "${built_lib}" "${runtime_dest}/${lib_name}"

  if [[ -n "${tmp_dir}" ]]; then
    rm -rf "${tmp_dir}"
  fi
}

case "${target_os}/${target_arch}" in
  linux/amd64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so.0"
    if command -v patchelf >/dev/null 2>&1 && ldd "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      patchelf --set-rpath '$ORIGIN/../lib' "${binary_path}"
    fi
    ;;
  linux/arm64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so"
    cp "${src_dir}/liblbug.so" "${runtime_root}/liblbug.so.0"
    if command -v patchelf >/dev/null 2>&1 && ldd "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      patchelf --set-rpath '$ORIGIN/../lib' "${binary_path}"
    fi
    ;;
  darwin/amd64 | darwin/arm64)
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib"
    cp "${src_dir}/liblbug.dylib" "${runtime_root}/liblbug.dylib"
    cp "${src_dir}/liblbug.dylib" "${runtime_root}/liblbug.0.dylib"
    if otool -L "${binary_path}" 2>/dev/null | grep -q 'liblbug'; then
      install_name_tool -delete_rpath "${src_dir}" "${binary_path}" 2>/dev/null || true
      install_name_tool -delete_rpath "${src_dir}/dynamic/darwin" "${binary_path}" 2>/dev/null || true
      install_name_tool -add_rpath "@loader_path/../lib" "${binary_path}"
    fi
    ;;
  windows/amd64)
    runtime_root="dist/runtime/${target_os}_${target_arch}/bin"
    mkdir -p "${runtime_root}"
    src_dir="${module_dir}/lib"
    cp "${src_dir}/lbug_shared.dll" "${runtime_root}/lbug_shared.dll"
    ;;
  *)
    echo "unsupported runtime target: ${target_os}/${target_arch}" >&2
    exit 1
    ;;
esac

stage_rax_runtime
stage_legacy_migration_helper
