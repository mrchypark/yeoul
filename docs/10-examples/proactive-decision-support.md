# Proactive Decision Support

This document defines the default Yeoul operating loop for normal work in this repository.

## Goal

Use Yeoul proactively so the agent can:
- recall relevant prior context before advising or implementing
- surface conflicts instead of repeating inconsistent decisions
- preserve durable outcomes without requiring an explicit save request every time

## Recommended database path

For normal work, prefer a single user-level database rather than a project-local `./yeoul.lbug`.

```bash
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
mkdir -p "$(dirname "$YEOUL_DB")"
```

Use `./yeoul.lbug` only for quickstarts, isolated tests, or disposable local experiments.

## Default loop

Yeoul can be used in the same role Graphiti plays for agents, but the operations are explicit rather than hidden inside an LLM extraction pipeline:

- episode ingest maps to `yeoul ingest episode`
- extraction maps to deliberate `yeoul fact assert` only when the subject, predicate, value, and provenance are clear
- automatic invalidation maps to `--cardinality one`, `yeoul fact supersede`, or `yeoul fact retract`
- graph-aware context construction maps to `yeoul search --include-related`, `yeoul context`, `yeoul timeline`, `yeoul provenance`, and `yeoul neighborhood`

Do not pretend that Yeoul silently extracted a graph. Search, record context/evidence episodes, assert decision facts, and update lifecycle explicitly.

### 1. Recall before advising

Before recommending a direction, interpreting status, or resolving a tradeoff:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "current context for this task" \
  --mode hybrid \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

Use narrower queries when the question is specific:

```bash
yeoul fact lookup --db "$YEOUL_DB" \
  --predicate HAS_STATUS \
  --include-inactive
```

### 2. Investigate history when memory may conflict

If prior memory appears inconsistent, incomplete, or time-sensitive:

```bash
yeoul timeline --db "$YEOUL_DB" --entity project:yeoul --descending
yeoul provenance --db "$YEOUL_DB" --fact fact_000001 --max-depth 2
```

Prefer surfacing the active state, the conflicting prior state, and the supporting episode rather than silently choosing one.

### 3. Work normally

During implementation, review, or debugging:
- keep using Yeoul when the conversation references earlier decisions, prior status, or repeated issues
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter

### 4. Capture durable outcomes at the end of a cycle

When a decision, fix, status change, or correction becomes clear, store a self-contained context/evidence episode even if the user did not explicitly ask to save it. Then assert the decision or durable state as a fact when the subject and predicate are clear.

Treat a message as a decision candidate when it does any of these:

- chooses between options, tradeoffs, tools, architectures, policies, defaults, or priorities
- accepts or rejects a recommendation
- changes a previous direction
- states a stable rule, constraint, preference, or operating policy
- produces a durable conclusion that future work should reuse

Do not record open brainstorming, low-confidence guesses, or temporary execution details as decisions. If the decision is implicit but likely durable, restate it and record only when the direction is clear.

For decisions, do not save only the conclusion when richer context is available.
Prefer a compact decision context episode that preserves how the choice was made.
Prefer the most reusable abstraction that is still true.
If the current project choice is one example of a broader pattern, store the broader pattern as the main decision and keep the project-specific detail as the current application.

Recommended decision context episode template:

```text
Topic: <the question or subject>
Context: <background, trigger, or problem>
Similar past decisions: <relevant prior decisions, if any>
Options:
1. <option A>
2. <option B>
Decision: <selected option and brief summary>
Why:
- <reason 1>
- <reason 2>
Tradeoffs:
- <important downside or rejected path>
Current application:
- <how this decision applies in the current project>
Revisit when:
- <condition that would change the decision>
Owner/status:
- <owner and proposed|active|validated|superseded|retracted>
Evidence:
- <supporting conversation, document, PR, test, benchmark, or source episode>
Observed at:
- <when this decision became known>
Valid from:
- <when this decision starts applying, if different>
```

```bash
yeoul ingest episode --db "$YEOUL_DB" \
  --kind decision_context \
  --source-kind codex_thread \
  --source-external-ref local-thread \
  --observed-at 2026-04-17T00:00:00Z \
  --content "Topic: proactive Yeoul usage for repository decision support. Context: decisions should remain reusable across sessions. Options: wait for explicit save requests, or proactively search and record durable outcomes. Decision: use proactive Yeoul memory. Why: it preserves prior decisions and catches conflicts. Tradeoffs: requires judgment to avoid low-signal records."
```

Use the episode for context and evidence, not as the lifecycle record for the decision. Then assert the decision as a fact.

Example decision context content:

```text
Topic: default Yeoul database location for normal work.
Context: project-local databases create too many files and split memory across repositories.
Similar past decisions: prefer a single long-lived memory store when the main goal is reuse across work.
Options:
1. keep one database per repository
2. use one user-level database for normal work
Decision: use one user-level database for normal work.
Why:
- reduces file sprawl
- keeps long-lived working memory in one place
- fits the long-term global-memory operating model better
Tradeoffs:
- search scoping must stay disciplined until CLI space and scope controls improve
Current application:
- Yeoul should default to $HOME/.local/share/yeoul/work-memory.lbug for normal work
Revisit when:
- CLI support for stronger per-project space selection or scoped retrieval becomes available
Owner/status:
- default repository agent behavior / active
Evidence:
- local agent instruction discussion
Observed at:
- 2026-04-17T00:00:00Z
```

Generalization example:

Bad:

```text
Decision: use vind for replited PocketBase chaos testing
```

Better:

```text
Topic: default chaos-test environment strategy
Context: the team needs a realistic but operable environment for failure testing
Options:
1. use a production-like replicated environment
2. use a simpler single-node fallback
Decision: prefer the closest production-like environment that still supports repeatable chaos scenarios
Why:
- keeps tests representative
- preserves a workable fallback when the ideal setup is unavailable
Current application:
- use vind first for replited PocketBase chaos tests
- fall back to single-node vind plus replica pod deletion chaos when needed
```

### 5. Assert the decision fact when clear

Plain-text episode ingest does not automatically create entities or facts from free text.
Use fact lifecycle commands for the decision statement, status, owner, and other durable state when the subject, predicate, and supporting episode are clear.

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_STATUS \
  --subject-id project_yeoul \
  --value-text "active" \
  --observed-at 2026-04-17T00:00:00Z \
  --supporting-episodes ep_000001
```

If the subject entity does not exist yet, create or update it while asserting the fact:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_DECISION \
  --upsert-subject \
  --subject-type Project \
  --subject-name Yeoul \
  --value-text "Use one user-level Yeoul database for normal work" \
  --supporting-episodes ep_000003
```

When `--observed-at` is omitted, `fact assert` inherits the first non-empty `observed_at` from the supporting episodes, then falls back to system time. Use an explicit `--observed-at` when the fact was observed at a different time. The CLI records the choice in metadata such as `observed_at_basis=system_time_default`.

Do not stop at episode-only for confirmed decisions, stable constraints, ownership/status/dependency changes, or corrections that future agents should retrieve through `fact lookup`, `timeline`, `provenance`, or conflict checks. Keep episode-only when the content is context, evidence, ambiguous, exploratory, or lacks a stable subject and predicate.

When a state changes, prefer superseding instead of overwriting:

```bash
yeoul fact supersede --confirm --db "$YEOUL_DB" \
  --id fact_000001 \
  --predicate HAS_STATUS \
  --subject-id project_yeoul \
  --value-text "completed" \
  --supporting-episodes ep_000010 \
  --reason "status changed"
```

For one-current-value slots, let the CLI supersede older active facts in the same slot:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate HAS_STATUS \
  --upsert-subject --subject-type Project --subject-name Yeoul --subject-stable-key project:yeoul \
  --value-text "ready for review" \
  --cardinality one \
  --supporting-episodes ep_000011
```

Use `--as-of` when asking what was known at a point in time. Use `--valid-at` when asking whether a fact's domain validity covered a point in time.

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
