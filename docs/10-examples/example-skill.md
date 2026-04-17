# Example Skill: Coding Agent Memory

```md
---
name: coding-agent-memory
version: 1
description: Use Yeoul to remember engineering decisions, task ownership changes, issue context, and repository facts.
---

# Purpose
Use Yeoul as the long-term temporal memory substrate for a coding-oriented agent.

# When to remember
- repository or architecture decisions
- owner changes
- issue state changes
- dependency changes
- task assignments
- confirmed bug root causes

# Do not remember
- greetings
- repeated acknowledgements
- low-signal chatter
- temporary speculation with no action value

# When to search
- when the user asks about prior decisions
- when the user asks who changed, owns, or decided something
- before proposing follow-up work on an existing project
- before answering “why” questions about the current state

# Retrieval preferences
- prefer recent active facts
- include provenance when presenting decisions
- include historical facts only when change history matters

# Output behavior
When memory is used, summarize the relevant facts and cite supporting episodes or sources where possible.
```
