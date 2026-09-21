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
3. Before the final answer, classify the outcome as no memory, episode-only context, a new durable fact, or a lifecycle change. This does not make every episode a fact.
4. For an authorized confirmed durable claim, do not stop after episode ingest because an entity is missing. Select namespace, type, canonical name, and stable key; reuse matching entities or assert with `--upsert-subject`. Use `--upsert-object` for a reusable named relationship target and `--value-text` for scalar or opaque text.
5. For Repository entities, use namespace `repo:<owner>/<repo>` and stable key `<owner>/<repo>` without repeating the namespace. Prefer real external IDs for other durable entities.
6. Check the active subject/predicate slot and possible conflicts before writing. Use `--cardinality one` only when overlapping active facts in that slot should be replaced.
7. Use `yeoul fact supersede` or `yeoul fact retract` for lifecycle changes instead of editing old facts. A `SUPERSEDES` assertion never substitutes for `fact supersede`.
8. Use `--as-of` for knowledge/lifecycle time and `--valid-at` for domain validity.
9. Read back with `fact lookup` and `provenance`; use `neighborhood` for relationships and `timeline` for lifecycle changes.

## Authority and privacy

Delegated agents may search and propose memory actions, but must not write a shared database without explicit authority for that write scope. Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win.

## Separation rule

Do not assume Yeoul Core knows anything about prompts, plans, tool protocols, or LLM behavior. Neither Yeoul Core nor policy YAML automatically extracts entities or promotes facts.
