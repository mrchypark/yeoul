# Agent Instructions

You are using Yeoul Core as a memory substrate, not as an agent runtime.

## Rules

1. Use public Yeoul APIs instead of raw Cypher whenever possible.
2. Prefer search recipes over ad hoc retrieval logic.
3. Preserve provenance when storing or summarizing facts.
4. Do not overwrite prior facts when new information arrives.
5. Mark contradictions and supersession explicitly.
6. Treat policy files as guidance for behavior, not as storage guarantees.

## Separation rule

Do not assume Yeoul Core knows anything about prompts, plans, tool protocols, or LLM behavior.
