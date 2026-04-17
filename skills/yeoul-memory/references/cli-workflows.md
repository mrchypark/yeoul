# Yeoul CLI Workflows

Use these patterns when the `yeoul-memory` skill is active.

## Search current context

```bash
yeoul search --db ./yeoul.lbug --query "Ladybug decision" --mode hybrid --include-related
```

Use `--policy-path` with `--recipe` when a pack should shape retrieval:

```bash
yeoul search --db ./yeoul.lbug \
  --query "recent project memory" \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

## Check whether a fact already exists

```bash
yeoul fact lookup --db ./yeoul.lbug \
  --subject-id project:yeoul \
  --predicate USES_STORAGE_ENGINE \
  --include-inactive
```

## Explain change history

```bash
yeoul timeline --db ./yeoul.lbug --entity project:yeoul --descending
yeoul provenance --db ./yeoul.lbug --fact fact_001 --max-depth 2
```

## Store a new episode

```bash
yeoul ingest episode --db ./yeoul.lbug \
  --kind note \
  --content "We decided to keep the core agent-free." \
  --source-kind note \
  --source-external-ref decision-log
```

For file-backed content:

```bash
yeoul ingest file --db ./yeoul.lbug \
  --kind note \
  --file ./notes/decision.txt \
  --source-kind file \
  --source-external-ref notes/decision.txt
```

## Record lifecycle changes

```bash
yeoul fact supersede --confirm --db ./yeoul.lbug \
  --id fact_old \
  --predicate HAS_STATUS \
  --subject-id project:yeoul \
  --value-text "beta" \
  --supporting-episodes ep_status_change \
  --reason "status changed"
```

```bash
yeoul fact retract --confirm --db ./yeoul.lbug \
  --id fact_bad \
  --reason "incorrect extraction"
```

## Safe maintenance

Preview before applying:

```bash
yeoul admin compact --db ./yeoul.lbug --json
yeoul entity merge-preview --db ./yeoul.lbug --json
```

Apply only with confirmation:

```bash
yeoul admin compact --confirm --apply --db ./yeoul.lbug
yeoul entity merge --confirm --db ./yeoul.lbug --target entity_a --source entity_b --reason "exact duplicate"
```
