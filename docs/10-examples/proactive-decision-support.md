# Proactive Decision Support

This document defines the default Yeoul operating loop for normal work in this repository.

## Goal

Use Yeoul proactively so the agent can:
- recall relevant prior context before advising or implementing
- surface conflicts instead of repeating inconsistent decisions
- preserve durable outcomes without requiring an explicit save request every time

## Recommended database path

For normal work, prefer a single user-level database rather than a project-local `./yeoul.ltdb`.

```bash
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.ltdb"
mkdir -p "$(dirname "$YEOUL_DB")"
```

Use `./yeoul.ltdb` only for quickstarts, isolated tests, or disposable local experiments. If only the legacy user-level `.lbug` database exists, keep using that path until migration and any rename are verified.

## Privacy and authority

Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win.

## Recipe policy path

For recipe-backed searches, select an absolute policy path before working. From this repository root, prefer its `agent-pack/`:

```bash
export YEOUL_POLICY_PATH="$(CDPATH= cd agent-pack && pwd -P)"
```

Relative `--policy-path` values are resolved from the current working directory, so use the selected absolute path. Yeoul has no default policy discovery and does not read `YEOUL_POLICY_PATH`; the variable is a shell convention that must be passed explicitly. Outside a repository with `agent-pack/`, set `YEOUL_POLICY_PATH` from the actual loaded `yeoul-memory` skill directory, which bundles `search_recipes.yaml`:

```bash
export YEOUL_LOADED_SKILL_DIR="/absolute/path/to/loaded/yeoul-memory"
export YEOUL_POLICY_PATH="$(CDPATH= cd "$YEOUL_LOADED_SKILL_DIR" && pwd -P)"
```

## Default loop

### 1. Recall before advising

Before recommending a direction, interpreting status, or resolving a tradeoff:

```bash
yeoul search --db "$YEOUL_DB" \
  --query "current context for this task" \
  --backend auto \
  --policy-path "$YEOUL_POLICY_PATH" \
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

When a decision, fix, status change, or correction becomes clear, store its source episode, then classify the outcome as episode-only context, a new durable fact, or a lifecycle change. For an authorized confirmed durable claim, do not stop after episode ingest merely because an entity is missing. This does not require every episode to become a fact.

For decisions, do not save only the conclusion when richer context is available.
Prefer a compact decision record that preserves how the choice was made.
Prefer the most reusable abstraction that is still true.
If the current project choice is one example of a broader pattern, store the broader pattern as the main decision and keep the project-specific detail as the current application.

Recommended decision template:

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
```

```bash
yeoul ingest episode --db "$YEOUL_DB" \
  --kind decision_note \
  --source-kind codex_thread \
  --source-external-ref local-thread \
  --observed-at 2026-04-17T00:00:00Z \
  --content "Decision: adopt proactive Yeoul usage for repository decision support. Search memory before recommendations and capture durable outcomes after decision or implementation cycles."
```

Use a plain-text episode when you want a reliable source record first.

Example decision note content:

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
- Yeoul should default to $HOME/.local/share/yeoul/work-memory.ltdb for normal work
Revisit when:
- CLI support for stronger per-project space selection or scoped retrieval becomes available
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

### 5. Promote structured state only when clear

Plain-text episode ingest does not automatically create entities or facts from free text.
When the subject and supporting episode are clear, select namespace, type, canonical name, and stable key explicitly. Reuse matching entities or create the subject and reusable relationship object atomically:

```bash
yeoul fact assert --db "$YEOUL_DB" \
  --predicate DEPENDS_ON \
  --upsert-subject \
  --subject-namespace repo:mrchypark/yeoul \
  --subject-type Project \
  --subject-name Yeoul \
  --subject-stable-key yeoul \
  --upsert-object \
  --object-namespace repo:mrchypark/rax \
  --object-type Repository \
  --object-name mrchypark/rax \
  --object-stable-key mrchypark/rax \
  --supporting-episodes ep_000001
```

For Repository entities, use namespace `repo:<owner>/<repo>` and stable key `<owner>/<repo>` without repeating the namespace. Prefer real external IDs for other durable entities. Use an object entity for a named referent future work should reuse or traverse; use `value_text` for a scalar, status, short conclusion, or opaque text.

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

A `SUPERSEDES` assertion never substitutes for `fact supersede`. Use `--as-of` for what Yeoul knew then and `--valid-at` for what was true then.

Before reporting completion:

1. Use `fact lookup` to confirm the active subject/predicate state.
2. Use `provenance` to confirm the supporting episode and source.
3. Use `neighborhood` to confirm relationship endpoints.
4. Use `timeline` after supersede or retract operations.

Subagents may search and propose memory actions, but must not write a shared database without explicit authority for that write scope. Neither Yeoul Core nor policy YAML performs automatic extraction or promotion.

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
