# Use Cases

This document defines the core product use cases Yeoul must support.

## UC-1: Remember a decision from a conversation

### Scenario
An application receives a chat or meeting summary that includes a project decision.

### Input
- raw message or summarized note
- source reference
- observed time
- optional entity hints

### Expected behavior
- create an Episode
- extract or attach relevant Entities
- assert one or more Facts
- link Facts back to the Episode and Source
- make the decision retrievable later by project, topic, or time window

### Acceptance notes
- old decisions must remain visible after newer decisions supersede them
- provenance must remain visible

## UC-2: Retrieve recent context for an entity

### Scenario
A developer asks, “What changed recently about project X?”

### Expected behavior
- locate the target Entity
- retrieve recent active Facts
- include supporting Episodes
- include changes, supersessions, and contradictions if relevant
- return a structured, rankable result

### Acceptance notes
- response must bias toward active and recent facts
- response must not silently discard historical state

## UC-3: Track changing ownership over time

### Scenario
A task, repository, or incident has multiple owners over time.

### Expected behavior
- represent ownership as one or more Facts
- use temporal fields and lifecycle links to record change history
- make current owner and previous owners separately queryable

### Acceptance notes
- do not overwrite an old fact in place
- use supersession or retraction semantics

## UC-4: Support agent policy without coupling to the engine

### Scenario
A coding agent must remember decisions and retrieve project context, but the rules live in files rather than code.

### Expected behavior
- agent loads skill and instruction files
- policy layer decides whether to remember or search
- Yeoul Core executes the storage and retrieval steps
- no LLM or prompt logic exists in the core

### Acceptance notes
- the same core should support multiple policy packs
- the engine should work even when no agent pack is present

## UC-5: Inspect why a fact exists

### Scenario
A user asks, “Why does memory say repository A depends on service B?”

### Expected behavior
- retrieve the Fact
- show status and time range
- show supporting Episode(s)
- show Source(s)
- expose derivation path clearly enough for debugging or audit

### Acceptance notes
- provenance traversal must be stable
- explanation must remain possible after compaction

## UC-6: Run fully local

### Scenario
A user runs Yeoul on a local machine with no server process and no network requirement.

### Expected behavior
- database opens in-process
- memory persists to local disk
- CLI can inspect the graph
- policy files are loaded from the local repository

### Acceptance notes
- core must not require external services
- daemon mode is optional, not mandatory

## UC-7: Rebuild derived graph state from episodes

### Scenario
A new ontology or deduplication policy is introduced and derived graph state needs recomputation.

### Expected behavior
- preserve original episodes
- replay or re-derive entities/facts using the new policy
- support dry-run preview where practical
- keep an audit trail of the rebuild

### Acceptance notes
- source episodes remain the durable substrate
- rebuild should be deterministic for the same input and policy version

## UC-8: Brief an agent before starting work

### Scenario
A coding or operations agent is about to implement, review, or investigate something and needs to know whether prior memory should change the approach.

### Expected behavior
- identify the target project, task, issue, or document from the current request
- retrieve active context that should influence the next action
- include standing instructions, constraints, recent blockers, and reusable workflow guidance when relevant
- return a concise briefing before the agent commits to a plan or tool sequence

### Acceptance notes
- retrieval must help before action, not only after a decision is already made
- non-decision context should be retrievable when it is likely to affect execution
- the agent should be able to say that no relevant prior memory was found

## Out-of-scope use cases
- distributed graph database clustering
- remote multi-tenant SaaS memory platform
- autonomous task planning
- hosted prompt orchestration
