---
name: yeoul-memory
description: Use when working in the Yeoul repository or when an agent should store, search, or explain durable local temporal memory with Yeoul. Covers when to remember, when to search first, which CLI commands to use, and how to report provenance and lifecycle state safely.
---

# Yeoul Memory

Use this skill when the task depends on prior project memory, durable decisions, fact lifecycle, or provenance in the Yeoul repo.

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

## Repo-specific workflow

In this repo, prefer the local CLI over inventing raw storage queries.

Read [references/cli-workflows.md](references/cli-workflows.md) when you need concrete command patterns for search, timeline, provenance, lifecycle changes, policy recipes, or maintenance flows.
