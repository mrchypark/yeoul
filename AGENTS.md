# Yeoul Decision Support

Use the `yeoul-memory` skill when a task depends on prior decisions, constraints, ownership, status changes, tradeoffs, or provenance in this repository.

When working with repository memory in normal use, use a single user-level Yeoul database instead of a project-local database file.
Default path: `$HOME/.local/share/yeoul/work-memory.lbug`

Project-local `./yeoul.lbug` is only for quickstart examples, isolated tests, or temporary debugging.
Prefer the workflows documented in `skills/yeoul-memory/SKILL.md` and `skills/yeoul-memory/references/cli-workflows.md`.
Use `docs/10-examples/proactive-decision-support.md` as the default operating loop for proactive Yeoul usage in this repository.

Use Yeoul proactively during normal work in this repository.
Do not wait for the user to explicitly ask for memory lookup or memory write when prior context is likely to improve the answer or preserve a durable outcome.

## Search first

Search Yeoul before answering when:
- the user is making or revisiting a decision
- prior decisions or constraints may affect the recommendation
- ownership or current status matters
- a new claim may conflict with existing memory
- change history or provenance matters

Prefer:
- `yeoul search` for broad recall
- `yeoul fact lookup` for subject or predicate checks
- `yeoul timeline` for change history
- `yeoul provenance` for explanation and supporting context

Default behavior:
- proactively search before recommendations, design choices, prioritization, status interpretation, or conflict resolution
- proactively search when the user refers to "before", "again", "still", "last time", "current status", or similar continuity cues
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter

## Decision support behavior

When helping with a decision:
- search for similar past decisions before proposing a new one
- summarize the decision question briefly
- retrieve relevant prior memory before recommending a direction
- present concrete options, implementation examples, and tradeoffs when multiple paths are viable
- make a recommendation only when the basis is clear
- if memory conflicts, surface the conflict instead of silently choosing one
- after the user decides, restate the selected direction briefly before recording it

## What to remember

Write to Yeoul only when the conversation contains durable information such as:
- a confirmed decision
- a stable constraint or rule
- an owner assignment
- a status change
- a correction with a reason
- a repeated problem and its resolution

Do not store:
- brainstorming that is still unsettled
- acknowledgements or low-signal chat
- unsupported guesses
- duplicate summaries of the same event

Do not treat every message as durable memory. Prefer quality over quantity.

Default behavior:
- when a durable outcome becomes clear, treat it as a memory-write candidate even if the user did not explicitly ask to save it
- prefer storing at the end of a decision, implementation, review, or correction cycle
- if the outcome is still ambiguous, defer writing until the state is clear instead of recording a weak summary

When recording a decision, prefer storing more than the conclusion alone.
Include, when available:
- `Topic`: the decision topic or question
- `Context`: the background or context
- `Similar past decisions`: relevant previous decisions or constraints
- `Options`: the main options considered
- `Decision`: the final decision and brief summary
- `Why`: the reason for choosing it
- `Tradeoffs`: important tradeoffs or rejected paths
- `Revisit when`: conditions that would justify revisiting the decision

Prefer the most reusable abstraction that is still true.
If a project-specific choice is only one application of a broader rule, store the broader rule as the main decision and treat the project-specific detail as an example or current application.
Do not let a product name, environment name, or one-off implementation detail become the main decision unless that specificity is itself what will matter later.

## Write rules

Use `ingest episode` or `ingest file` for source records.
Use `fact assert` only when the subject and supporting episode are clear.
Do not overwrite old facts when the state changes.
Prefer lifecycle operations such as `fact supersede` and `fact retract` with an explicit reason.
Preserve provenance when storing or summarizing memory.

## Response rules

When answering from memory:
- prefer active facts by default
- mention time context when it matters
- mention provenance when the basis matters
- distinguish active state from superseded or retracted history

When memory use materially changes the answer:
- say that prior context was checked
- summarize the relevant prior decision, constraint, or conflict briefly
- keep the explanation concise unless the user asks for detail
