# Yeoul CLI Workflows

Use these patterns when the `yeoul-memory` skill is active.

## Recommended database path

For normal work, prefer a single user-level database rather than a project-local `./yeoul.lbug`.

```bash
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
export YEOUL_GROUP="project:yeoul"
export YEOUL_PROJECT_ID="repo:mrchypark-yeoul:project:yeoul"
mkdir -p "$(dirname "$YEOUL_DB")"
```

Use `./yeoul.lbug` only for quickstarts, isolated tests, or disposable local experiments.
`$YEOUL_GROUP` scopes searches and episode writes. `fact assert` does not take `--group-id`; use `$YEOUL_PROJECT_ID` or repo namespace `repo:mrchypark/yeoul` plus stable keys for fact subjects instead.

## Search current context

```bash
yeoul search --db "$YEOUL_DB" --query "Ladybug decision" --mode hybrid --group-id "$YEOUL_GROUP" --include-related
```

Use `--policy-path` with `--recipe` when a pack should shape retrieval:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "recent project memory" \
  --group-id "$YEOUL_GROUP" \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

## Check whether a fact already exists

```bash
yeoul fact lookup --db "$YEOUL_DB" \
  --subject-id "$YEOUL_PROJECT_ID" \
  --predicate USES_STORAGE_ENGINE \
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

Use episode ingest as the context/evidence step, not the decision lifecycle record. If the episode supports a confirmed decision, stable constraint, status change, ownership change, dependency relation, or correction that future agents should retrieve through fact lookup, continue with structured fact assertion.

Episode content should preserve the background, evidence, context, and source needed to understand the memory later. Do not reduce it to only the final decision when supporting context is available.
Match episode detail to the fact type: decisions need context/options/why/tradeoffs; status needs previous/new state and as-of time; corrections need wrong/right/reason; benchmarks need setup/metric/result/decision impact; ownership needs owner/scope; rules need scope/exceptions.
First decide whether the exchange contains a fact-worthy claim or only episode-worthy context. If it is fact-worthy but missing the subject, claim, scope, time/status, or supporting context needed for a reliable fact, ask a focused clarification instead of asserting a weak fact.

Do not store secrets, credentials, personal/customer data, or verbatim private content, even with confirmation; redact or omit it. Before implicit writes to the global database, confirm exact fact text and scope unless the user explicitly requested the write in the current turn and the content is non-sensitive and repo-scoped.

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

Use `fact supersede --confirm` only when a previously asserted current-status fact needs a lifecycle update. Keep the original contract and outcome episodes as provenance.

For file-backed content:

```bash
yeoul ingest file --db "$YEOUL_DB" \
  --kind note \
  --file ./notes/decision.txt \
  --source-kind file \
  --source-external-ref notes/decision.txt
```

## Assert clear decisions or rules as facts

Facts are promoted claims, not a copy of every episode. Assert a fact only when it is a confirmed durable claim with a stable subject and at least one supporting episode. Keep raw status, progress, benchmark results, implementation logs, review notes, and exploratory context episode-only unless they establish durable state, a reusable conclusion, or a rule.

When the subject entity already exists, assert the fact directly:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_DECISION \
  --subject-id "$YEOUL_PROJECT_ID" \
  --value-text "Yeoul uses one user-level database for normal work" \
  --observed-at 2026-04-17T00:00:00Z \
  --supporting-episodes ep_000003
```

When the subject is clear but the entity has not been created yet, let the CLI create or update it before asserting the fact:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_DECISION \
  --upsert-subject \
  --subject-namespace repo:mrchypark/yeoul \
  --subject-type Project \
  --subject-name Yeoul \
  --subject-stable-key yeoul \
  --value-text "Yeoul uses one user-level database for normal work" \
  --supporting-episodes ep_000003
```

If `--observed-at` is omitted, `fact assert` uses the first non-empty `observed_at` from the supporting episodes, then falls back to system time. Pass `--observed-at` explicitly when the fact observation time differs from the episode time. The CLI records the basis in metadata, for example `observed_at_basis=system_time_default`.

For relationships, the object can be upserted in the same command:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate USES_STORAGE_ENGINE \
  --upsert-subject --subject-namespace repo:mrchypark/yeoul --subject-type Project --subject-name Yeoul --subject-stable-key yeoul \
  --upsert-object --object-namespace repo:mrchypark/yeoul --object-type Database --object-name Ladybug --object-stable-key ladybug \
  --supporting-episodes ep_000001
```

Keep episode-only when the content is context, evidence, ambiguous, exploratory, status/progress, implementation detail, benchmark output, review note, or lacks a stable subject and predicate.

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
