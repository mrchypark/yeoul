#!/bin/sh

set -eu

target_os="${1:-linux}"
target_arch="${2:-amd64}"
runtime_root="dist/runtime/${target_os}_${target_arch}"
fixture="pkg/yeoul/testdata/v022-lifecycle.lbug.gz"
work_root="$(mktemp -d)"
trap 'rm -rf "${work_root}"' EXIT

yeoul_bin=""
for candidate in "$PWD"/dist/*/bin/yeoul; do
  [ -x "${candidate}" ] || continue
  if [ -n "${yeoul_bin}" ]; then
    printf 'multiple Yeoul binaries found: %s and %s\n' "${yeoul_bin}" "${candidate}" >&2
    exit 1
  fi
  yeoul_bin="${candidate}"
done
if [ -z "${yeoul_bin}" ]; then
  printf 'no built Yeoul binary found under dist/*/bin/yeoul\n' >&2
  exit 1
fi

mkdir -p "${work_root}/package/bin" "${work_root}/package/libexec"
cp "${yeoul_bin}" "${work_root}/package/bin/yeoul"
cp -R "${runtime_root}/lib" "${work_root}/package/lib"
cp -R "${runtime_root}/libexec/ladybug-v0131" "${work_root}/package/libexec/ladybug-v0131"
gzip -dc "${fixture}" >"${work_root}/work-memory.lbug"

runtime_env_name="LD_LIBRARY_PATH"
if [ "${target_os}" = "darwin" ]; then
  runtime_env_name="DYLD_LIBRARY_PATH"
fi
migration_json="$(env "${runtime_env_name}=${work_root}/package/lib" \
  "${work_root}/package/bin/yeoul" admin migrate-db --db "${work_root}/work-memory.lbug" --json)"
printf '%s' "${migration_json}" | jq -e '
  .migrated == true and
  .source_driver == "ladybug" and
  .target_driver == "lattice" and
  (.backup_path | length > 0)
' >/dev/null

counts_json="$("${work_root}/package/bin/yeoul" inspect counts --db "${work_root}/work-memory.lbug" --json)"
printf '%s' "${counts_json}" | jq -e '
  .counts.entities == 1 and
  .counts.episodes == 1 and
  .counts.facts == 2 and
  .counts.sources == 1
' >/dev/null

"${work_root}/package/bin/yeoul" index build \
  --db "${work_root}/work-memory.lbug" \
  --root "${work_root}/index" \
  --json | jq -e '
    .counts.entity_revisions == 1 and
    .counts.fact_revisions == 4
  ' >/dev/null

"${work_root}/package/bin/yeoul" search \
  --db "${work_root}/work-memory.lbug" \
  --query preserves \
  --json | jq -e 'any(.hits[]; .hit_type == "episode" and .record_id == "ep_000001")' >/dev/null

second_json="$("${work_root}/package/bin/yeoul" admin migrate-db --db "${work_root}/work-memory.lbug" --json)"
printf '%s' "${second_json}" | jq -e '
  .migrated == false and
  .source_driver == "lattice" and
  .target_driver == "lattice"
' >/dev/null

backup_count="$(find "${work_root}" -maxdepth 1 -name 'work-memory.lbug.ladybug-backup-*' | wc -l | tr -d ' ')"
[ "${backup_count}" -eq 1 ]

printf 'v0.2.2 migration bridge smoke test passed\n'
