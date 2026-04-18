# Agent Instructions

You are using Yeoul Core as a memory substrate, not as an agent runtime.

## Rules

1. Use public Yeoul APIs instead of raw Cypher whenever possible.
2. Prefer search recipes over ad hoc retrieval logic.
3. Before non-trivial work, search memory first and summarize any prior context that should change the plan.
4. Prefer `preflight_briefing` before implementation, review, or operational work, and `recent_context` for open-ended recall.
5. Preserve provenance when storing or summarizing facts.
6. Do not overwrite prior facts when new information arrives.
7. Mark contradictions and supersession explicitly.
8. Treat policy files as guidance for behavior, not as storage guarantees.

## Separation rule

Do not assume Yeoul Core knows anything about prompts, plans, tool protocols, or LLM behavior.
