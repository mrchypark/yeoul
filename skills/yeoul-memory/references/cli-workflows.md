# Yeoul CLI Workflows

Use these patterns when the `yeoul-memory` skill is active.

## Recommended database path

For normal work, prefer a single user-level database rather than a project-local `./yeoul.lbug`.

```bash
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
mkdir -p "$(dirname "$YEOUL_DB")"
```

Use `./yeoul.lbug` only for quickstarts, isolated tests, or disposable local experiments.

## Search current context

```bash
yeoul search --db "$YEOUL_DB" --query "Ladybug decision" --mode hybrid --include-related
```

Use `--policy-path` with `--recipe` when a pack should shape retrieval:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "recent project memory" \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

## Check whether a fact already exists

```bash
yeoul fact lookup --db "$YEOUL_DB" \
  --subject-id project:yeoul \
  --predicate USES_STORAGE_ENGINE \
  --include-inactive
```

## Explain change history

```bash
yeoul timeline --db "$YEOUL_DB" --entity project:yeoul --descending
yeoul provenance --db "$YEOUL_DB" --fact fact_001 --max-depth 2
```

## Store a new episode

```bash
yeoul ingest episode --db "$YEOUL_DB" \
  --kind note \
  --content "We decided to keep the core agent-free." \
  --source-kind note \
  --source-external-ref decision-log
```

Use episode ingest as the provenance step, not always the final memory shape. If the episode contains a confirmed decision, stable constraint, status change, ownership change, dependency relation, or correction that future agents should retrieve through fact lookup, continue with structured promotion.

For decisions, prefer recording structured context instead of only the conclusion:

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

For file-backed content:

```bash
yeoul ingest file --db "$YEOUL_DB" \
  --kind note \
  --file ./notes/decision.txt \
  --source-kind file \
  --source-external-ref notes/decision.txt
```

## Promote clear state to a fact

When the subject entity already exists, assert the fact directly:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_DECISION \
  --subject-id project:yeoul \
  --value-text "Yeoul uses one user-level database for normal work" \
  --observed-at 2026-04-17T00:00:00Z \
  --supporting-episodes ep_000003
```

When the subject is clear but the entity has not been created yet, let the CLI create or update it before asserting the fact:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_DECISION \
  --upsert-subject \
  --subject-type Project \
  --subject-name Yeoul \
  --value-text "Yeoul uses one user-level database for normal work" \
  --supporting-episodes ep_000003
```

If `--observed-at` is omitted, `fact assert` uses the first non-empty `observed_at` from the supporting episodes, then falls back to system time. Pass `--observed-at` explicitly when the fact observation time differs from the episode time. The CLI records the basis in metadata, for example `observed_at_basis=system_time_default`.

For relationships, the object can be upserted in the same command:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate USES_STORAGE_ENGINE \
  --upsert-subject --subject-type Project --subject-name Yeoul \
  --upsert-object --object-type Database --object-name Ladybug \
  --supporting-episodes ep_000001
```

Keep episode-only when the content is ambiguous, exploratory, or lacks a stable subject and predicate.

## Record lifecycle changes

```bash
yeoul fact supersede --confirm --db "$YEOUL_DB" \
  --id fact_old \
  --predicate HAS_STATUS \
  --subject-id project:yeoul \
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
