---
name: yeoul-memory
description: Use when working in the Yeoul repository or when an agent should store, search, or explain durable local temporal memory with Yeoul. Covers when to remember, when to search first, which CLI commands to use, and how to report provenance and lifecycle state safely.
---

# Yeoul Memory

Use this skill when the task depends on prior project memory, working context, durable guidance, fact lifecycle, or provenance in the Yeoul repo.

## Search first

Search Yeoul before answering when:
- the user asks what was decided before
- the task depends on prior project constraints, status, preferences, workflow guidance, or recent issues
- you are about to start non-trivial work and prior memory may change the plan, tools, or output shape
- a new fact may conflict with existing memory
- you need provenance or change history

Prefer:
- `yeoul search` for broad recall
- `yeoul fact lookup` for subject/predicate checks
- `yeoul timeline` for change history
- `yeoul provenance` for explanation
- `yeoul neighborhood` for local graph context
- `recent_context` for open-ended project recall
- `preflight_briefing` before non-trivial work when prior memory may change the plan
- `contradiction_check` before asserting a fact that may conflict with existing memory

## Remember deliberately

Store memory only when the content is likely to matter later:
- explicit decisions
- stable constraints
- working preferences or standing instructions
- recurring workflow guidance
- ownership, status, or focus changes
- open loops that future work should check
- corrections or retractions
- repeated problems and resolutions

Do not store:
- acknowledgements
- low-signal chatter
- unsupported guesses
- destructive corrections without a reason

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
- When starting non-trivial work, say briefly whether prior context was checked and what it changed.

## Repo-specific workflow

In this repo, prefer the local CLI over inventing raw storage queries.

Read [references/cli-workflows.md](references/cli-workflows.md) when you need concrete command patterns for search, timeline, provenance, lifecycle changes, policy recipes, or maintenance flows.
