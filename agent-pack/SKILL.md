# Yeoul Memory Skill

Use Yeoul when an agent needs durable, local, temporal memory across sessions.

## Remember when

- the user makes a decision
- ownership changes
- project status changes
- a fact is likely to matter later
- provenance should be preserved

## Search when

- the current task depends on prior project context
- the user asks what was decided before
- the agent needs recent facts tied to an entity, project, or source
- contradiction checks are needed before asserting a new fact

## Ignore when

- the message is only an acknowledgement
- the content is duplicate low-signal chatter
- the information is transient and not worth durable storage

## Citation rule

When memory results are used, summarize them with time context and provenance rather than exposing raw internal graph details.
