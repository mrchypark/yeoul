#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <tag>" >&2
  exit 1
fi

tag="$1"
source_repo="${SOURCE_REPO:-mrchypark/yeoul}"
tap_repo="${HOMEBREW_TAP_REPO:-mrchypark/homebrew-tap}"
tap_token="${HOMEBREW_TAP_GITHUB_TOKEN:-}"

if [[ -z "${tap_token}" ]]; then
  echo "HOMEBREW_TAP_GITHUB_TOKEN is required" >&2
  exit 1
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd gh
need_cmd git
need_cmd jq
need_cmd curl

export GH_TOKEN="${tap_token}"

source_release_json="$(gh api "repos/${source_repo}/releases/tags/${tag}")"
tap_repo_json="$(gh api "repos/${tap_repo}")"

tap_branch="$(jq -r '.default_branch' <<< "${tap_repo_json}")"
if [[ -z "${tap_branch}" || "${tap_branch}" == "null" ]]; then
  echo "failed to resolve default branch for ${tap_repo}" >&2
  exit 1
fi

asset_url() {
  local asset_name="$1"
  jq -r --arg name "${asset_name}" '.assets[] | select(.name == $name) | .browser_download_url' <<< "${source_release_json}" | head -n1
}

download_to() {
  local url="$1"
  local dest="$2"
  curl -fsSL -H "Authorization: Bearer ${tap_token}" -H "Accept: application/octet-stream" "${url}" -o "${dest}"
}

asset_version="${tag#v}"
darwin_amd_archive="yeoul_${asset_version}_darwin_amd64.tar.gz"
darwin_arm_archive="yeoul_${asset_version}_darwin_arm64.tar.gz"
linux_amd_archive="yeoul_${asset_version}_linux_amd64.tar.gz"
linux_arm_archive="yeoul_${asset_version}_linux_arm64.tar.gz"

darwin_amd_checksums="checksums_darwin-amd64.txt"
darwin_arm_checksums="checksums_darwin-arm64.txt"
linux_amd_checksums="checksums_linux-amd64.txt"
linux_arm_checksums="checksums_linux-arm64.txt"

darwin_amd_url="$(asset_url "${darwin_amd_archive}")"
darwin_arm_url="$(asset_url "${darwin_arm_archive}")"
linux_amd_url="$(asset_url "${linux_amd_archive}")"
linux_arm_url="$(asset_url "${linux_arm_archive}")"

for required in "${darwin_amd_url}" "${darwin_arm_url}" "${linux_amd_url}" "${linux_arm_url}"; do
  if [[ -z "${required}" || "${required}" == "null" ]]; then
    echo "missing release asset required for Homebrew formula" >&2
    exit 1
  fi
done

temp_dir="$(mktemp -d)"
trap 'rm -rf "${temp_dir}"' EXIT

download_to "$(asset_url "${darwin_amd_checksums}")" "${temp_dir}/${darwin_amd_checksums}"
download_to "$(asset_url "${darwin_arm_checksums}")" "${temp_dir}/${darwin_arm_checksums}"
download_to "$(asset_url "${linux_amd_checksums}")" "${temp_dir}/${linux_amd_checksums}"
download_to "$(asset_url "${linux_arm_checksums}")" "${temp_dir}/${linux_arm_checksums}"

checksum_for() {
  local checksum_file="$1"
  local archive_name="$2"
  awk -v name="${archive_name}" '$2 == name { print $1 }' "${checksum_file}"
}

darwin_amd_sha="$(checksum_for "${temp_dir}/${darwin_amd_checksums}" "${darwin_amd_archive}")"
darwin_arm_sha="$(checksum_for "${temp_dir}/${darwin_arm_checksums}" "${darwin_arm_archive}")"
linux_amd_sha="$(checksum_for "${temp_dir}/${linux_amd_checksums}" "${linux_amd_archive}")"
linux_arm_sha="$(checksum_for "${temp_dir}/${linux_arm_checksums}" "${linux_arm_archive}")"

for required in "${darwin_amd_sha}" "${darwin_arm_sha}" "${linux_amd_sha}" "${linux_arm_sha}"; do
  if [[ -z "${required}" ]]; then
    echo "missing checksum required for Homebrew formula" >&2
    exit 1
  fi
done

tap_clone="${temp_dir}/tap"
git clone "https://x-access-token:${tap_token}@github.com/${tap_repo}.git" "${tap_clone}" >/dev/null 2>&1

mkdir -p "${tap_clone}/Formula"

cat > "${tap_clone}/Formula/yeoul.rb" <<EOF
class Yeoul < Formula
  desc "Local-first temporal graph memory engine"
  homepage "https://github.com/${source_repo}"
  version "${asset_version}"

  on_macos do
    if Hardware::CPU.arm?
      url "${darwin_arm_url}"
      sha256 "${darwin_arm_sha}"
    end
    if Hardware::CPU.intel?
      url "${darwin_amd_url}"
      sha256 "${darwin_amd_sha}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${linux_arm_url}"
      sha256 "${linux_arm_sha}"
    end
    if Hardware::CPU.intel?
      url "${linux_amd_url}"
      sha256 "${linux_amd_sha}"
    end
  end

  def install
    libexec.install Dir["*"]

    runtime_env = {}
    if OS.mac?
      runtime_env["DYLD_LIBRARY_PATH"] = libexec/"lib"
    elsif OS.linux?
      runtime_env["LD_LIBRARY_PATH"] = libexec/"lib"
    end

    (bin/"yeoul").write_env_script libexec/"bin/yeoul", runtime_env
    (bin/"yeould").write_env_script libexec/"bin/yeould", runtime_env
  end

  test do
    assert_match "Usage:", shell_output("#{bin}/yeoul help")
  end
end
EOF

git -C "${tap_clone}" config user.name "github-actions[bot]"
git -C "${tap_clone}" config user.email "41898282+github-actions[bot]@users.noreply.github.com"

if git -C "${tap_clone}" diff --quiet -- Formula/yeoul.rb; then
  echo "Homebrew formula already up to date"
  exit 0
fi

git -C "${tap_clone}" add Formula/yeoul.rb
git -C "${tap_clone}" commit -m "Brew formula update for yeoul version ${tag}" >/dev/null
git -C "${tap_clone}" push origin "HEAD:${tap_branch}" >/dev/null

echo "Published Formula/yeoul.rb to ${tap_repo}@${tap_branch}"
