# Agent Instructions

You are using Yeoul Core as a memory substrate, not as an agent runtime.

## Rules

1. Use public Yeoul APIs instead of raw Cypher whenever possible.
2. Prefer search recipes over ad hoc retrieval logic.
3. Preserve provenance when storing or summarizing facts.
4. Do not overwrite prior facts when new information arrives.
5. Mark contradictions and supersession explicitly.
6. Treat policy files as guidance for behavior, not as storage guarantees.

## Explicit memory flow

Use episodes for context and evidence. Use facts for decisions, status, ownership, dependencies, and other reusable state.

When a conversation produces durable memory:

1. Search first with `yeoul search`, `yeoul fact lookup`, `yeoul timeline`, or `yeoul provenance`.
2. Store self-contained context with `yeoul ingest episode`.
3. Assert the durable claim with `yeoul fact assert --supporting-episodes ...` only when the subject and predicate are clear.
4. Use `--cardinality one` only when active facts in the same subject/predicate slot should be replaced.
5. Use `yeoul fact supersede` or `yeoul fact retract` for lifecycle changes instead of editing old facts.
6. Use `--as-of` for knowledge/lifecycle time and `--valid-at` for domain validity.
7. Treat `rax` as a derived retrieval index; Ladybug-backed Yeoul records remain canonical.

## Separation rule

Do not assume Yeoul Core knows anything about prompts, plans, tool protocols, or LLM behavior.
