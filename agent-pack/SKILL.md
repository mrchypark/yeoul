# Yeoul Memory Skill

Use Yeoul when an agent needs durable, local, temporal memory across sessions.

## Remember when

- the user makes a decision
- the user states a standing preference or instruction
- a workflow rule or preflight check should be reused later
- ownership changes
- project status changes
- a recurring issue, pitfall, or workaround becomes clear
- a fact is likely to matter later
- provenance should be preserved

## Search when

- the current task depends on prior project context
- the agent is about to start non-trivial work and should check for relevant context first
- the user asks what was decided before
- the agent needs recent facts tied to an entity, project, or source
- contradiction checks are needed before asserting a new fact

## Prefer recipes

- prefer `recent_context` for open-ended project recall
- prefer `preflight_briefing` before implementation, review, or operational work
- prefer `contradiction_check` before asserting a fact that may conflict with existing memory

## Ignore when

- the message is only an acknowledgement
- the content is duplicate low-signal chatter
- the information is transient and not worth durable storage

## Citation rule

When memory results are used, summarize them with time context and provenance rather than exposing raw internal graph details.
