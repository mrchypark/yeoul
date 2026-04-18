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

For a before-you-start briefing, prefer the dedicated recipe:

```bash
yeoul search --db ./yeoul.lbug \
  --query "what should I check before working on release automation?" \
  --policy-path ./agent-pack \
  --recipe preflight_briefing \
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
