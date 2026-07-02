---
name: yeoul-memory
description: Use when working in the Yeoul repository or when an agent should store, search, or explain durable local temporal memory with Yeoul. Covers when to remember, when to search first, which CLI commands to use, and how to report provenance and lifecycle state safely.
---

# Yeoul Memory

Use this skill when the task depends on prior project memory, durable decisions, fact lifecycle, or provenance in the Yeoul repo.

## Default database path

For normal work, prefer a single user-level Yeoul database instead of a project-local database file.

Default path:
- `$HOME/.local/share/yeoul/work-memory.lbug`

Project-local `./yeoul.lbug` is only for quickstarts, isolated tests, or temporary debugging.

## Search first

Search Yeoul before answering when:
- the user asks what was decided before
- the task depends on prior project constraints or status
- a new fact may conflict with existing memory
- you need provenance or change history

Prefer:
- `yeoul search` for broad recall
- `yeoul fact lookup` for subject/predicate checks
- `yeoul timeline` for change history
- `yeoul provenance` for explanation
- `yeoul neighborhood` for local graph context

Default behavior:
- proactively search before recommendations, design choices, prioritization, status interpretation, or conflict resolution
- proactively search when the user refers to earlier decisions, previous attempts, current status, or continuity across work
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter

When a decision is required:
- search for similar past decisions first
- present current options and realistic alternatives
- include implementation examples and tradeoffs when useful
- restate the user's chosen direction before recording it
- expect to reuse the recorded decision later

## Remember deliberately

Store memory only when the content is likely to matter later:
- explicit decisions
- stable constraints
- ownership or status changes
- corrections or retractions
- repeated problems and resolutions

Do not store:
- acknowledgements
- low-signal chatter
- unsupported guesses
- destructive corrections without a reason

Default behavior:
- when a durable outcome becomes clear, treat it as a memory-write candidate even if the user did not explicitly ask to save it
- prefer storing at the end of a decision, implementation, review, or correction cycle
- if the outcome is still ambiguous, defer writing until the state is clear instead of recording a weak summary

## Fact extraction loop

For every substantive exchange, look for fact candidates that should be retrievable later.
Fact candidates include confirmed decisions, durable rules or constraints, current status, owners, corrections or retractions, repeated problems and resolutions, dependencies or relationships, stable preferences, definitions or terminology, and validated evaluation or benchmark conclusions.
First decide whether the exchange contains a fact-worthy claim or only episode-worthy context.
If it is fact-worthy but missing the subject, claim, scope, time/status, or supporting context needed for a reliable fact, ask a focused clarification instead of asserting a weak fact.

## Episode and fact boundary

Episodes are source records. Use them to preserve background, evidence, context, and provenance.
Facts are promoted claims. Promote confirmed durable claims that need fact lookup.
Do not promote every episode to a fact.
Keep raw progress, benchmark results, implementation logs, review notes, and exploratory context as episodes unless they establish durable state, a reusable conclusion, a rule, a relationship, a stable preference, or a definition.
Every fact must have at least one supporting episode.
Episode content should fit the fact type: decisions need context/options/why/tradeoffs; status needs previous/new state and as-of time; corrections need wrong/right/reason; benchmarks need setup/metric/result/decision impact; ownership needs owner/scope; dependencies/relationships need subject/object/relation/evidence; preferences need holder/scope/default; definitions need term/scope/meaning; repeated problems need symptom/root cause/resolution; rules need scope/exceptions.

When recording a decision, prefer storing more than the conclusion alone.
Include, when available:
- `Topic`: the decision topic or question
- `Context`: the background or context
- `Similar past decisions`: relevant previous decisions or constraints
- `Options`: the main options considered
- `Decision`: the final decision and brief summary
- `Why`: the reason for choosing it
- `Tradeoffs`: important tradeoffs or rejected paths
- `Current application`: how the decision applies in the present project or task
- `Revisit when`: conditions that would justify revisiting the decision

Prefer the most reusable abstraction that is still true.
If the current project choice is one application of a broader pattern, store the broader pattern as the main decision and treat the project-specific detail as the current application.
For decisions shaped like "use X for Y", do not make `X` the whole fact unless the user gave the reusable selection criterion or explicitly said the exact tool choice is the durable rule. Store the selection criterion as the fact and keep `X for Y` as `Current application`; if the criterion is missing, ask or keep it episode-only.
Do not let a one-off tool name, environment name, or implementation detail become the main decision unless that specificity is exactly what future work will need.

## Write rules

- Use `ingest episode` or `ingest file` for source episodes.
- Use `fact assert` only when subject and supporting episodes are clear.
- Use `fact supersede --confirm` for state changes rather than overwriting.
- Use `fact retract --confirm` only with an explicit reason.
- Use `admin compact` as dry-run first; treat apply as maintenance, not normal editing.

## Response rules

- Prefer active facts for the default answer.
- If facts conflict, surface the conflict.
- Mention time context when it matters.
- Mention provenance or supporting episodes when explaining why something is believed.
- Treat duplicate-marked entities as historical aliases, not canonical current answers.
- When memory use materially changes the answer, say briefly that prior context was checked and summarize the relevant decision, constraint, or conflict.

## Repo-specific workflow

In this repo, prefer the local CLI over inventing raw storage queries.

Read [references/cli-workflows.md](references/cli-workflows.md) when you need concrete command patterns for search, timeline, provenance, lifecycle changes, policy recipes, or maintenance flows.
