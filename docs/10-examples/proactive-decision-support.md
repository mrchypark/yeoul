# Proactive Decision Support

This document defines the default Yeoul operating loop for normal work in this repository.

## Goal

Use Yeoul proactively so the agent can:
- recall relevant prior context before advising or implementing
- surface conflicts instead of repeating inconsistent decisions
- preserve durable outcomes without requiring an explicit save request every time

## Default loop

### 1. Recall before advising

Before recommending a direction, interpreting status, or resolving a tradeoff:

```bash
yeoul search --db ./yeoul.lbug \
  --query "current context for this task" \
  --mode hybrid \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

Use narrower queries when the question is specific:

```bash
yeoul fact lookup --db ./yeoul.lbug \
  --predicate HAS_STATUS \
  --include-inactive
```

### 2. Investigate history when memory may conflict

If prior memory appears inconsistent, incomplete, or time-sensitive:

```bash
yeoul timeline --db ./yeoul.lbug --entity project:yeoul --descending
yeoul provenance --db ./yeoul.lbug --fact fact_000001 --max-depth 2
```

Prefer surfacing the active state, the conflicting prior state, and the supporting episode rather than silently choosing one.

### 3. Work normally

During implementation, review, or debugging:
- keep using Yeoul when the conversation references earlier decisions, prior status, or repeated issues
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter

### 4. Capture durable outcomes at the end of a cycle

When a decision, fix, status change, or correction becomes clear, store a source episode even if the user did not explicitly ask to save it.

```bash
yeoul ingest episode --db ./yeoul.lbug \
  --kind decision_note \
  --source-kind codex_thread \
  --source-external-ref local-thread \
  --observed-at 2026-04-17T00:00:00Z \
  --content "Decision: adopt proactive Yeoul usage for repository decision support. Search memory before recommendations and capture durable outcomes after decision or implementation cycles."
```

Use a plain-text episode when you want a reliable source record first.

### 5. Promote structured state only when clear

Plain-text episode ingest does not automatically create entities or facts from free text.
Use fact lifecycle commands only when the subject and supporting episode are clear.

```bash
yeoul fact assert --db ./yeoul.lbug \
  --predicate HAS_STATUS \
  --subject-id project_yeoul \
  --value-text "active" \
  --supporting-episodes ep_000001
```

When a state changes, prefer superseding instead of overwriting:

```bash
yeoul fact supersede --confirm --db ./yeoul.lbug \
  --id fact_000001 \
  --predicate HAS_STATUS \
  --subject-id project_yeoul \
  --value-text "completed" \
  --supporting-episodes ep_000010 \
  --reason "status changed"
```

## Save heuristics

Write memory when the conversation contains:
- a confirmed decision
- a stable rule or constraint
- an ownership change
- a status change
- a correction with a reason
- a repeated problem and the resolution

Do not write memory for:
- open-ended brainstorming
- acknowledgements
- weak guesses
- duplicate summaries

## Response pattern

When Yeoul materially affects the answer:
- state briefly that prior context was checked
- summarize the most relevant prior decision, status, or conflict
- mention time context or provenance when it changes the recommendation

Keep the explanation short unless the user asks for detail.
