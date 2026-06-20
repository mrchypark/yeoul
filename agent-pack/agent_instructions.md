# Agent Instructions

You are using Yeoul Core as a memory substrate, not as an agent runtime.

## Rules

1. Use public Yeoul APIs instead of raw Cypher whenever possible.
2. Prefer search recipes over ad hoc retrieval logic.
3. Preserve provenance when storing or summarizing facts.
4. Do not overwrite prior facts when new information arrives.
5. Mark contradictions and supersession explicitly.
6. Treat policy files as guidance for behavior, not as storage guarantees.

## Graphiti-style memory flow

Graphiti performs episode ingest, extraction, invalidation, and context construction inside the agent stack. With Yeoul, perform those steps explicitly through the CLI:

1. Search first with `yeoul search` or build bounded context with `yeoul context`.
2. Store self-contained context and evidence with `yeoul ingest episode`.
3. Record the decision itself as a supported fact with `yeoul fact assert --supporting-episodes ...`.
4. Use `--cardinality one` only when all overlapping active facts for the same subject/predicate should be replaced.
5. Use `yeoul fact supersede` or `yeoul fact retract` for lifecycle changes instead of editing old facts or injecting reserved metadata.
6. Use `--as-of` for knowledge/lifecycle time and `--valid-at` for domain validity; do not mix what was known then with what was true then.
7. Use repository-qualified namespaces plus `--subject-stable-key` or `--object-stable-key` when writing to the global database.
8. Treat `--backend auto` as the default search path; rax is a derived retrieval index, not canonical truth.
9. In this repository, scope global-memory searches and episode writes with `--group-id project:yeoul`; scope fact writes with canonical project subject ID `repo:mrchypark-yeoul:project:yeoul` or repo namespace `repo:mrchypark/yeoul` plus stable keys. This is an agent fail-closed convention, not a storage-layer security boundary.
10. If a harness provides a specific Yeoul binary or DB path, run that exact binary and DB in every memory command; do not run `command -v yeoul`, `yeoul --help`, or any bare `yeoul` fallback unless no binary path was provided.

Do not pretend that an LLM silently extracted a graph. If the agent infers a decision, fact, contradiction, or replacement, state it briefly and record it with explicit Yeoul commands.
Do not treat the episode itself as the decision record. Use episodes for context/evidence and facts for decision statements, status, ownership, and lifecycle.

## Decision detection

Use Yeoul as a global decision record. Treat a conversation turn as a decision candidate when it contains one of these signals:

1. The user chooses between options, tradeoffs, tools, architectures, policies, defaults, or priorities.
2. The user accepts or rejects a recommendation.
3. The user changes a previous direction.
4. The user states a stable rule, constraint, preference, or operating policy.
5. The work produces a durable conclusion that future tasks should reuse.

Do not store secrets, credentials, personal/customer data, or verbatim private content, even with confirmation; redact or omit it. Before implicit writes to the global database, confirm exact fact text and scope unless the user explicitly requested the write in the current turn and the content is non-sensitive and repo-scoped.

Do not record open brainstorming, low-confidence guesses, or temporary execution details as decisions. If the decision is implicit but likely durable, restate it and ask or proceed only when the user's direction is clear.

## Decision record quality

Before asserting a decision fact, search for similar prior decisions and record the supporting context episode. Preserve more than the conclusion:
Before asking the user, fill every field that can be inferred from the conversation, retrieved memory, code, tests, docs, or command output. Ask only for missing information that is required for a complete and truthful record, such as unclear scope, final choice, reason, owner/status, or revisit condition. Do not use missing optional details as a reason to save a thin record.
If the current turn only proposes a durable choice or asks for advice, do not silently postpone the write and call that confirmation. Say the exact missing question first, then record only after the user confirms or the final choice is otherwise explicit.
For multi-turn transcripts or replay evaluations, process each user turn as a live turn: search or answer first, ask if confirmation is missing, write immediately after a later turn confirms, then verify the record can be retrieved before using it.
Use a field-labeled `decision_context` episode for decisions. If a field below is unknown but required to make the record truthful, ask before asserting the fact; if it is optional and unavailable, write `unknown` or `none found` rather than inventing it.

- `Topic`: the reusable decision question, not just the current product or file name
- `Context`: why the decision came up
- `Similar past decisions`: relevant prior memory or `none found`
- `Options`: realistic alternatives considered
- `Decision`: the selected direction
- `Why`: the reason this option won
- `Tradeoffs`: what was rejected or made worse
- `Current application`: the immediate project/task where this applies
- `Revisit when`: evidence that should reopen the decision
- `Owner/status`: who owns follow-up and whether the decision is proposed, active, validated, superseded, or retracted
- `Evidence`: supporting conversation, document, PR, test, benchmark, or source episode
- `Observed at`: when the decision became known
- `Valid from`: when the decision starts applying, if different from observation time

Prefer the most general true rule. Put project-specific details under `Current application` unless the detail itself is the durable decision.

Manage decisions over time instead of rewriting them:

1. Use `yeoul ingest episode` for the decision context and evidence.
2. Use `yeoul fact assert --cardinality one` only when all overlapping active facts for the same subject/predicate should be replaced; use explicit `yeoul fact supersede --confirm --reason` when one known fact should be linked to one replacement.
3. Use `yeoul fact supersede` when a later decision replaces an earlier one.
4. Use `yeoul fact retract` only when a recorded decision was wrong or should not be trusted.
5. Use `yeoul timeline` and `yeoul provenance` before adjusting a decision so the reason and history stay visible.

## Separation rule

Do not assume Yeoul Core knows anything about prompts, plans, tool protocols, or LLM behavior.
