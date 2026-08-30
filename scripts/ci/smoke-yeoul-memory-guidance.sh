#!/bin/sh
set -eu

yeoul_bin=${YEOUL_BIN:-"${HOME}/.local/bin/yeoul"}
if [ ! -x "${yeoul_bin}" ]; then
  printf 'YEOUL_BIN is not executable: %s\n' "${yeoul_bin}" >&2
  exit 1
fi

temp_root=$(CDPATH= cd "${TMPDIR:-/tmp}" && pwd -P)
case "${temp_root}" in
  /) smoke_pattern=/yeoul-memory-guidance.XXXXXX ;;
  *) smoke_pattern=${temp_root}/yeoul-memory-guidance.XXXXXX ;;
esac

smoke_root=$(mktemp -d "${smoke_pattern}")
smoke_db=${smoke_root}/memory.ltdb
smoke_state=testing

report_exit() {
  status=$?
  trap - EXIT HUP INT TERM
  case "${smoke_state}" in
    testing)
      printf 'Yeoul memory guidance smoke failed; retained: %s\n' "${smoke_root}" >&2
      ;;
    cleanup)
      printf 'Yeoul memory guidance assertions passed, but cleanup was incomplete: %s\n' "${smoke_root}" >&2
      ;;
  esac
  exit "${status}"
}

trap report_exit EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

"${yeoul_bin}" init --db "${smoke_db}"
"${yeoul_bin}" ingest episode --db "${smoke_db}" \
  --id ep-yeoul-memory-guidance-smoke \
  --kind note \
  --content "The memory smoke project depends on the example dependency repository." \
  --source-kind note \
  --source-external-ref yeoul-memory-guidance-smoke \
  --group-id repo:smoke-test

fact_json=$("${yeoul_bin}" fact assert --db "${smoke_db}" \
  --predicate DEPENDS_ON \
  --upsert-subject \
  --subject-namespace repo:example/memory-smoke \
  --subject-type Project \
  --subject-name "Memory smoke" \
  --subject-stable-key memory-smoke \
  --upsert-object \
  --object-namespace repo:example/dependency \
  --object-type Repository \
  --object-name example/dependency \
  --object-stable-key example/dependency \
  --supporting-episodes ep-yeoul-memory-guidance-smoke \
  --json)

fact_id=$(printf '%s\n' "${fact_json}" | sed -n 's/^[[:space:]]*"id": "\([^"]*\)".*$/\1/p' | sed -n '1p')
subject_id=$(printf '%s\n' "${fact_json}" | sed -n 's/^[[:space:]]*"subject_id": "\([^"]*\)".*$/\1/p' | sed -n '1p')
object_id=$(printf '%s\n' "${fact_json}" | sed -n 's/^[[:space:]]*"object_id": "\([^"]*\)".*$/\1/p' | sed -n '1p')
test -n "${fact_id}"
test -n "${subject_id}"
test -n "${object_id}"

counts_json=$("${yeoul_bin}" inspect counts --db "${smoke_db}" --json)
printf '%s\n' "${counts_json}" | grep -q '"episodes": 1'
printf '%s\n' "${counts_json}" | grep -q '"entities": 2'
printf '%s\n' "${counts_json}" | grep -q '"facts": 1'

lookup_json=$("${yeoul_bin}" fact lookup --db "${smoke_db}" --subject-id "${subject_id}" --predicate DEPENDS_ON --json)
printf '%s\n' "${lookup_json}" | grep -Fq "${object_id}"
printf '%s\n' "${lookup_json}" | grep -Fq ep-yeoul-memory-guidance-smoke

provenance_json=$("${yeoul_bin}" provenance --db "${smoke_db}" --fact "${fact_id}" --max-depth 2 --json)
printf '%s\n' "${provenance_json}" | grep -Fq ASSERTS
printf '%s\n' "${provenance_json}" | grep -Fq FROM_SOURCE

neighborhood_json=$("${yeoul_bin}" neighborhood --db "${smoke_db}" --fact "${fact_id}" --hops 1 --json)
printf '%s\n' "${neighborhood_json}" | grep -Fq "${subject_id}"
printf '%s\n' "${neighborhood_json}" | grep -Fq "${object_id}"

search_json=$("${yeoul_bin}" search --db "${smoke_db}" --backend core --type fact --query DEPENDS_ON --json)
printf '%s\n' "${search_json}" | grep -Fq "${fact_id}"

smoke_parent=$(CDPATH= cd "$(dirname "${smoke_root}")" && pwd -P)
smoke_name=$(basename "${smoke_root}")
if [ "${smoke_parent}" != "${temp_root}" ]; then
  printf 'Refusing cleanup outside resolved temp root: %s\n' "${smoke_root}" >&2
  exit 1
fi
case "${smoke_name}" in
  yeoul-memory-guidance.?*) ;;
  *)
    printf 'Refusing unexpected cleanup target: %s\n' "${smoke_root}" >&2
    exit 1
    ;;
esac

smoke_state=cleanup
(
  CDPATH= cd "${temp_root}"
  rm -r "${smoke_name}"
)
smoke_state=passed
trap - EXIT HUP INT TERM
printf 'Yeoul memory guidance smoke passed\n'
