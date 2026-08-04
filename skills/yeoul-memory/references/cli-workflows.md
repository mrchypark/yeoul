# Yeoul CLI Workflows

Use these patterns when the `yeoul-memory` skill is active.

## Recommended database path

For normal work, prefer a single user-level database rather than a project-local `./yeoul.lbug`.

```bash
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
export YEOUL_GROUP="project:yeoul"
export YEOUL_PROJECT_ID="project:yeoul"
<<<<<<< Updated upstream
export YEOUL_REPOSITORY_ID="repo:mrchypark-yeoul:repository:mrchypark-yeoul"
mkdir -p "$(dirname "$YEOUL_DB")"
```

Use `./yeoul.lbug` only for quickstarts, isolated tests, or disposable local experiments.
`$YEOUL_GROUP` scopes searches and episode writes. `$YEOUL_PROJECT_ID` preserves the current established project subject ID in the user database; apply the canonical namespace and stable-key conventions below to newly upserted entities. Use `$YEOUL_PROJECT_ID` for broad project continuity and `$YEOUL_REPOSITORY_ID` for repo-specific fact lookups. `fact assert` does not take `--group-id`; use an existing subject ID or upsert with repo namespace `repo:mrchypark/yeoul` plus stable keys.

## Search current context

```bash
yeoul search --db "$YEOUL_DB" --query "Ladybug decision" --backend auto --group-id "$YEOUL_GROUP" --include-related
```

## Verify local install

After installing or upgrading Yeoul locally, verify the wrapper and the real user-level database:

```bash
command -v yeoul
sed -n '1,5p' "$HOME/.local/bin/yeoul"
yeoul inspect counts --db "$YEOUL_DB" --json
yeoul search --db "$YEOUL_DB" --query "recent Yeoul memory" --backend auto --group-id "$YEOUL_GROUP" --limit 3
```

If the new binary cannot open an existing Ladybug database, migrate through a fresh database instead of leaving the wrapper pointed at an unusable install:

```bash
old_bin="$HOME/.local/share/yeoul/v0.2.2/bin/yeoul"
new_bin="$HOME/.local/share/yeoul/v0.2.3/bin/yeoul"
backup_dir="$HOME/.local/share/yeoul/backups/upgrade-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"
"$old_bin" admin export --db "$YEOUL_DB" --out "$backup_dir/export.json" --json
"$new_bin" init --db "$backup_dir/work-memory.new.lbug" --json
"$new_bin" admin import --db "$backup_dir/work-memory.new.lbug" --in "$backup_dir/export.json" --json --confirm
"$new_bin" inspect counts --db "$backup_dir/work-memory.new.lbug" --json
```

Keep the original database under the backup directory before replacing `$YEOUL_DB`. If inactive or superseded facts matter, inspect them with the old binary using `fact lookup --include-inactive` and preserve or reconstruct lifecycle state before the replacement.

Use `--policy-path` with `--recipe` when a pack should shape retrieval:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "recent project memory" \
  --group-id "$YEOUL_GROUP" \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

Before non-trivial Yeoul repo work, run this preflight before planning, bulk exploration, implementation, or a final answer:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "what should I check before working on this task?" \
  --group-id "$YEOUL_GROUP" \
  --policy-path ./agent-pack \
  --recipe preflight_briefing \
  --include-related
```

If `preflight_briefing` is missing, run `yeoul policy list-recipes --path ./agent-pack`, use `recent_context` once as a fallback, and report that the policy pack is stale.

## Check whether a fact already exists

```bash
yeoul fact lookup --db "$YEOUL_DB" \
  --subject-id "$YEOUL_PROJECT_ID" \
  --predicate DECIDED \
  --group-id "$YEOUL_GROUP" \
  --include-inactive
```

## Explain change history

```bash
yeoul timeline --db "$YEOUL_DB" --entity "$YEOUL_PROJECT_ID" --descending
yeoul provenance --db "$YEOUL_DB" --fact fact_001 --max-depth 2
```

## Store a new episode

```bash
yeoul ingest episode --db "$YEOUL_DB" \
  --kind note \
  --content "We decided to keep the core agent-free." \
  --source-kind note \
  --source-external-ref decision-log \
  --group-id "$YEOUL_GROUP"
```

Use episode ingest as the context/evidence step, not the decision lifecycle record. If the episode supports a confirmed durable claim that future agents should retrieve through fact lookup, continue with structured fact assertion. Fact-worthy claims include decisions, stable constraints or rules, status changes, ownership changes, dependency or relationship claims, corrections or retractions, stable preferences, definitions or terminology, repeated problem/resolution conclusions, and validated evaluation or benchmark conclusions.

Episode content should preserve the background, evidence, context, and source needed to understand the memory later. Do not reduce it to only the final decision when supporting context is available.
Match episode detail to the fact type: decisions need context/options/why/tradeoffs; status needs previous/new state and as-of time; corrections need wrong/right/reason; benchmarks need setup/metric/result/decision impact; ownership needs owner/scope; dependencies/relationships need subject/object/relation/evidence; preferences need holder/scope/default; definitions need term/scope/meaning; repeated problems need symptom/root cause/resolution; rules need scope/exceptions.
First decide whether the exchange contains a fact-worthy claim or only episode-worthy context. If it is fact-worthy but missing the subject, claim, scope, time/status, or supporting context needed for a reliable fact, ask a focused clarification instead of asserting a weak fact.

Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win. Before implicit writes to the global database, confirm exact fact text and scope unless the user explicitly requested the write in the current turn and the content is non-sensitive and repo-scoped.

Before the final answer for a substantive cycle, classify the outcome as no memory, episode-only context, a new durable fact, or a lifecycle change. For an authorized confirmed durable claim, do not stop after episode ingest because an entity is missing. Select the subject namespace, type, canonical name, and stable key; the predicate; and either a reusable object entity or `value_text`. This checkpoint does not make every episode a fact.

Read-only searches and memory-action planning may be delegated. A delegated agent must not write a shared or user-level database unless the user, repository policy, or owning agent explicitly delegated that exact write scope. Neither Yeoul Core nor policy YAML automatically extracts entities or promotes facts.

For decisions, record self-contained context before asserting the decision fact:

```text
Topic: default Yeoul database location for normal work
Context: project-local databases create too many files and split memory across repositories
Similar past decisions: prefer a single long-lived memory store when reuse across work matters
Options:
1. keep one database per repository
2. use one user-level database for normal work
Decision: use one user-level database for normal work
Why:
- reduces file sprawl
- keeps long-lived memory in one place
Tradeoffs:
- retrieval scoping must remain disciplined until CLI space and scope controls improve
Current application:
- use $HOME/.local/share/yeoul/work-memory.lbug as the normal default
Revisit when:
- stronger per-project space selection becomes available
```

Prefer the most reusable abstraction that is still true.
If the current project choice is only one example of a broader rule, store the broader rule as the main decision and keep the project-specific detail under `Current application`.
For decisions shaped like "use X for Y", do not make `X` the whole fact unless the user gave the reusable selection criterion or explicitly said the exact tool choice is the durable rule. Store the selection criterion as the fact and keep `X for Y` under `Current application`; if the criterion is missing, ask or keep it episode-only.

## Store a falsifiable change contract

Use a change contract when a workflow, harness, skill, prompt, evaluator, or automation change should be checked against a later result. Record the prediction and the possible regression before treating the change as validated.

```text
Topic: harness timeout recovery contract
Contract ID: contract_2026_05_13_harness_timeout_recovery
Context:
- evaluation task retry-on-timeout currently fails after the first timeout
Change:
- update the harness retry policy in skills/domain/example/SKILL.md
Prediction:
- retry-on-timeout should pass in the next evaluation run
- timeout-related manual intervention should decrease
Regression risk:
- tasks that depend on immediate failure may run longer
- stale browser sessions may be reused too aggressively
Falsification condition:
- retry-on-timeout still fails for the same reason
- any unrelated timeout-sensitive task regresses
Rollback plan:
- revert the retry-policy block in skills/domain/example/SKILL.md
Evaluation result:
- pending
Status: active
```

Store it as an episode unless there is already a clear subject, predicate, and supporting episode set for a fact:

```bash
yeoul ingest episode --db "$YEOUL_DB" \
  --kind note \
  --content "$(< contract.md)" \
  --source-kind note \
  --source-external-ref "change-contract:contract_2026_05_13_harness_timeout_recovery" \
  --group-id "$YEOUL_GROUP"
```

After the next evaluation, add an outcome episode instead of overwriting the original contract:

```text
Topic: harness timeout recovery contract outcome
Contract ID: contract_2026_05_13_harness_timeout_recovery
Evaluation result:
- retry-on-timeout passed
- one immediate-failure task regressed by waiting for retries
Prediction match:
- primary prediction matched
- regression risk materialized
Action:
- falsified and reverted the retry-policy block
Status: reverted
```

Use `fact supersede --confirm` only when a previously asserted current-status fact needs a lifecycle update. Keep the original contract and outcome episodes as provenance. A `SUPERSEDES` assertion never substitutes for `fact supersede`. Use `--as-of` for what Yeoul knew then and `--valid-at` for what was true then.

For file-backed content:

```bash
yeoul ingest file --db "$YEOUL_DB" \
  --kind note \
  --file ./notes/decision.txt \
  --source-kind file \
  --source-external-ref notes/decision.txt
```

## Assert clear durable claims as facts

Facts are promoted claims, not a copy of every episode. Assert a fact only when it is a confirmed durable claim with a stable subject and at least one supporting episode. Keep raw status, progress, benchmark results, implementation logs, review notes, and exploratory context episode-only unless they establish durable state, a reusable conclusion, a rule, a relationship, a stable preference, or a definition.

When the subject entity already exists, assert the fact directly:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate DECIDED \
  --subject-id "$YEOUL_PROJECT_ID" \
  --upsert-object \
  --object-namespace repo:mrchypark/yeoul \
  --object-type Decision \
  --object-name "Use one user-level Yeoul database" \
  --object-stable-key user-level-database-default \
  --observed-at 2026-04-17T00:00:00Z \
  --supporting-episodes ep_000003
```

When the subject is clear but the entity has not been created yet, let the CLI create or update the subject and a reusable object atomically:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate DECIDED \
  --upsert-subject \
  --subject-namespace repo:mrchypark/yeoul \
  --subject-type Project \
  --subject-name Yeoul \
  --subject-stable-key yeoul \
  --upsert-object \
  --object-namespace repo:mrchypark/yeoul \
  --object-type Decision \
  --object-name "Use one user-level Yeoul database" \
  --object-stable-key user-level-database-default \
  --supporting-episodes ep_000003
```

If `--observed-at` is omitted, `fact assert` uses the first non-empty `observed_at` from the supporting episodes, then falls back to system time. Pass `--observed-at` explicitly when the fact observation time differs from the episode time. The CLI records the basis in metadata, for example `observed_at_basis=system_time_default`.

For another relationship, the object can be upserted in the same command:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate DEPENDS_ON \
  --upsert-subject --subject-namespace repo:mrchypark/yeoul --subject-type Project --subject-name Yeoul --subject-stable-key yeoul \
  --upsert-object --object-namespace repo:mrchypark/rax --object-type Repository --object-name mrchypark/rax --object-stable-key mrchypark/rax \
  --supporting-episodes ep_000001
```

Use a named referent as an object entity when future work should reuse, filter, or traverse it. Use `value_text` for a scalar, status, short conclusion, or opaque text. Do not create an entity for every noun.

Select namespace, type, canonical name, and stable key explicitly. For Repository entities, use namespace `repo:<owner>/<repo>` and stable key `<owner>/<repo>` without repeating the namespace. Prefer real external IDs for other durable entities. Ontology patterns include `Project DECIDED Decision`, `Person OWNS Repository` or `Task`, component `DEPENDS_ON` component, and record `MENTIONED_IN Document`; a domain pack may use `Role` as the owner type when it declares that type.

Keep episode-only when the content is only context, evidence, ambiguous, exploratory, raw status/progress, implementation detail, benchmark output, or review note and does not establish a durable claim. If it appears fact-worthy but lacks a stable subject/predicate or required context, ask a focused clarification before asserting.

## Record lifecycle changes

```bash
yeoul fact supersede --confirm --db "$YEOUL_DB" \
  --id fact_old \
  --predicate HAS_STATUS \
  --subject-id "$YEOUL_PROJECT_ID" \
  --value-text "beta" \
  --supporting-episodes ep_status_change \
  --reason "status changed"
```

```bash
yeoul fact retract --confirm --db "$YEOUL_DB" \
  --id fact_bad \
  --reason "incorrect extraction"
```

## Verify a completed write

After writing, read the structured memory back before reporting completion:

```bash
yeoul fact lookup --db "$YEOUL_DB" --subject-id "$YEOUL_PROJECT_ID" --predicate DECIDED --include-inactive --json
yeoul provenance --db "$YEOUL_DB" --fact fact_001 --max-depth 2 --json
yeoul neighborhood --db "$YEOUL_DB" --fact fact_001 --hops 1 --json
yeoul timeline --db "$YEOUL_DB" --fact fact_001 --descending --json
```

Use `fact lookup` and `provenance` for every fact write, `neighborhood` for relationships, and `timeline` for lifecycle changes. Use `yeoul context` when a bounded agent-ready retrieval bundle is needed.

## Safe maintenance

Preview before applying:

```bash
yeoul admin compact --db "$YEOUL_DB" --json
yeoul entity merge-preview --db "$YEOUL_DB" --json
```

Apply only with confirmation:

```bash
yeoul admin compact --confirm --apply --db "$YEOUL_DB"
yeoul entity merge --confirm --db "$YEOUL_DB" --target entity_a --source entity_b --reason "exact duplicate"
```
