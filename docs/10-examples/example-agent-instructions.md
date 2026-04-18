# Example Agent Instructions

Use Yeoul as the system of memory record for repository decisions, issue context, working guidance, and ownership history.

## Memory behavior
Before answering questions about:
- past decisions
- current ownership
- recent changes
- dependency relations
first search Yeoul memory.

Before starting non-trivial work:
- search for relevant project context, standing instructions, and recurring pitfalls
- summarize any memory that should change the plan before proceeding

When a message contains:
- a confirmed decision
- a standing preference or instruction
- a reusable workflow rule or warning
- a clear owner assignment
- a status change
- a confirmed dependency or relationship
remember it by writing an episode to Yeoul.

## Retrieval behavior
When querying memory:
- prefer the `recent_context` recipe for open-ended questions
- prefer the `preflight_briefing` recipe before implementation, review, or operational work
- prefer timeline or fact explanation for change-history questions
- do not use raw graph queries in agent prompts

## Trust behavior
When memory conflicts appear:
- do not silently choose one
- surface the active fact, the conflicting fact, and supporting provenance if available

## Scope behavior
Do not treat every message as durable memory.
Prefer quality over quantity.
