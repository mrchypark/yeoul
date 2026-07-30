# Yeoul Memory Skill

Use Yeoul when an agent needs durable, local, temporal memory across sessions.

## Remember when

- a substantive exchange contains a fact candidate likely to matter later
- the user confirms a decision, durable rule, constraint, preference, definition, or terminology
- ownership, current status, dependencies, relationships, corrections, or retractions change
- a repeated problem gets a reusable resolution
- an evaluation or benchmark establishes a validated conclusion
- provenance should be preserved for later fact lookup

## Search when

- the current task depends on prior project context
- the user asks what was decided before
- the agent needs recent facts tied to an entity, project, or source
- contradiction checks are needed before asserting a new fact

## Fact extraction loop

For every substantive exchange, first decide whether it contains a fact-worthy claim or only episode-worthy context.
Fact candidates include confirmed decisions, durable rules or constraints, current status, owners, corrections or retractions, repeated problems and resolutions, dependencies or relationships, stable preferences, definitions or terminology, and validated evaluation or benchmark conclusions.
If the exchange is fact-worthy but missing the subject, claim, scope, time/status, or supporting context needed for a reliable fact, ask a focused clarification instead of asserting a weak fact.

Episodes preserve background, evidence, context, source, and provenance. Facts are promoted durable claims, not copies of every episode. Every fact needs at least one supporting episode.
Episode content should fit the fact type: decisions need context/options/why/tradeoffs; status needs previous/new state and as-of time; corrections need wrong/right/reason; benchmarks need setup/metric/result/decision impact; ownership needs owner/scope; dependencies/relationships need subject/object/relation/evidence; preferences need holder/scope/default; definitions need term/scope/meaning; repeated problems need symptom/root cause/resolution; rules need scope/exceptions.

## Structured-memory checkpoint

Before the final answer for a substantive cycle, classify the outcome as no memory, episode-only context, a new durable fact, or a lifecycle change. For an authorized confirmed durable claim, do not stop after episode ingest because an entity is missing: select namespace, type, canonical name, and stable key; reuse or upsert the subject; and choose a reusable object entity or `value_text`. Check active facts and conflicts, complete the lifecycle operation, then read back with `fact lookup` and `provenance`; use `neighborhood` for relationships and `timeline` for changes. This does not make every episode a fact.

For Repository entities, use namespace `repo:<owner>/<repo>` and stable key `<owner>/<repo>` without repeating the namespace. Prefer real external IDs for other durable entities. A `SUPERSEDES` assertion never substitutes for `fact supersede`.

Read-only work may be delegated. A delegated agent may propose a memory action but must not write a shared database without explicit authority for that write scope. Neither Yeoul Core nor policy YAML performs automatic entity extraction or fact promotion.

## Ignore when

- the message is only an acknowledgement
- the content is duplicate low-signal chatter
- the information is transient and not worth durable storage
- the content contains secrets, credentials, or private keys
- sensitive personal or customer data lacks explicit authorization for a defined write scope or cannot be minimized or redacted

Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win.

## Citation rule

When memory results are used, summarize them with time context and provenance rather than exposing raw internal graph details.
