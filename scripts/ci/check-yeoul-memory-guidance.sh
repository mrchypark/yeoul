#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
repo_root=$(CDPATH= cd "${script_dir}/../.." && pwd)

require_text() {
  if ! grep -Fq -e "$2" "${repo_root}/$1"; then
    printf 'missing Yeoul memory guidance in %s: %s\n' "$1" "$2" >&2
    exit 1
  fi
}

primary=skills/yeoul-memory/SKILL.md
require_text "${primary}" '## Structured-memory checkpoint'
require_text "${primary}" 'do not stop after episode ingest'
require_text "${primary}" 'select the subject namespace, type, canonical name, and stable key'
require_text "${primary}" '`fact assert --upsert-subject`'
require_text "${primary}" '`--upsert-object`'
require_text "${primary}" 'Model a named referent as an object entity'
require_text "${primary}" 'A `SUPERSEDES` assertion never substitutes for `fact supersede`.'
require_text "${primary}" '`--as-of` for what Yeoul knew then'
require_text "${primary}" 'verify with `fact lookup` and `provenance`'
require_text "${primary}" '## Authority and delegation'
require_text "${primary}" 'Neither Yeoul Core nor policy YAML automatically extracts entities or promotes facts'
require_text "${primary}" 'Always omit secrets, credentials, and private keys.'
require_text "${primary}" 'Non-sensitive stable preferences and commitments may be stored'

require_text AGENTS.md 'classify the outcome as no memory, episode-only context, a new durable fact, or a lifecycle change'
require_text AGENTS.md 'only the owning agent may perform confirmed repo-scoped shared-database writes'
require_text agent-pack/SKILL.md 'This does not make every episode a fact.'
require_text agent-pack/agent_instructions.md 'Use `--upsert-object` for a reusable named relationship target'
require_text skills/yeoul-memory/references/cli-workflows.md 'Ontology patterns include `Project DECIDED Decision`'
require_text skills/yeoul-memory/references/cli-workflows.md '## Verify a completed write'
require_text skills/yeoul-memory/references/cli-workflows.md 'export YEOUL_PROJECT_ID="project:yeoul"'
require_text skills/yeoul-memory/references/cli-workflows.md '--object-namespace repo:mrchypark/rax'
require_text docs/10-examples/proactive-decision-support.md 'Use `neighborhood` to confirm relationship endpoints.'
require_text docs/10-examples/proactive-decision-support.md '--object-namespace repo:mrchypark/rax'
require_text agent-pack/integrations/codex/install.md 'is the canonical distributable source'
require_text agent-pack/integrations/codex/install.md 'scripts/ci/smoke-yeoul-memory-guidance.sh'
require_text evals/yeoul-memory/skillopt-entity-promotion.md 'Secrets, credentials, and private keys are always omitted; sensitive personal or customer data requires explicit authorization for a defined scope and must be minimized or redacted.'

if [ ! -x "${repo_root}/scripts/ci/smoke-yeoul-memory-guidance.sh" ]; then
  printf 'Yeoul memory guidance smoke is not executable\n' >&2
  exit 1
fi

printf 'Yeoul memory guidance content contract passed\n'
