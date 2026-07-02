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

## Ignore when

- the message is only an acknowledgement
- the content is duplicate low-signal chatter
- the information is transient and not worth durable storage

## Citation rule

When memory results are used, summarize them with time context and provenance rather than exposing raw internal graph details.
