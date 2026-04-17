# Glossary

This glossary defines the terms used across Yeoul Core and Yeoul Agent Pack.

## Product terms

### Yeoul
The overall project. A local-first Temporal Graph Memory Engine written in Go and backed by Ladybug.

### Yeoul Core
The storage and memory engine. It has no built-in LLM calls, prompt execution, agent planner, or autonomous runtime.

### Yeoul Agent Pack
A set of skills, instruction files, ontology templates, episode rules, and search recipes designed for AI agent integrations.

### Embedded mode
A deployment mode where Yeoul runs inside the host application's process and directly owns the Ladybug database object.

### Daemon mode
A deployment mode where a local Yeoul service process owns the Ladybug database and other local processes access it through an API.

## Memory model terms

### Episode
An observed unit of input that Yeoul records. Examples: a chat message, tool result, meeting summary, issue update, or document note.

### Entity
A reusable thing that can be referred to across episodes. Examples: person, project, repository, file, task, organization, incident.

### Fact
A first-class claim derived from one or more episodes. Facts exist as nodes because they need explicit time, provenance, status, and lifecycle tracking.

### Provenance
The traceable chain that explains where a fact came from, how it was derived, and which episodes or sources support it.

### Source
The origin container of one or more episodes. Examples: chat thread, issue tracker item, file path, document, ticket.

### Relationship
A typed graph edge connecting nodes such as Episode, Entity, Fact, and Source.

### Temporal graph
A graph where the history of facts and relationships matters, including when something was observed, asserted, superseded, or retracted.

### Ontology
The set of allowed or recommended entity types, predicates, classification hints, and deduplication keys.

### Deduplication
The process of deciding that two observed records refer to the same conceptual entity.

### Compaction
The process of reducing redundant graph state without losing provenance or historical validity.

## Query and retrieval terms

### Search recipe
A declarative retrieval strategy that specifies how a class of questions should be answered from memory.

### Neighborhood query
A graph query that expands outward from an entity or fact using bounded hops and filters.

### Hybrid search
A retrieval strategy that combines lexical, graph-structural, and semantic signals.

### Contradiction check
A retrieval pattern used to detect existing active facts that may conflict with a newly asserted fact.

### Recall
The act of retrieving relevant memory from Yeoul.

### Remember
The act of converting new input into episodes, entities, facts, and relationships.

## Operational terms

### Schema migration
A versioned change to the Yeoul storage schema.

### Checkpoint
A storage-level persistence event that merges transient write state into durable database files.

### Retention policy
Rules controlling how long episodes, facts, and derived state remain stored or active.

### Redaction
The removal or masking of sensitive content before or after ingest.

### Replay
Re-ingesting or re-deriving graph state from stored episodes or exported logs.

## Policy-layer terms

### Skill file
A human-readable document that tells an external agent when and how to use Yeoul.

### Agent instructions
Policy text that defines expected memory behavior for a specific class of agent.

### Episode rules
A declarative file that describes which incoming events become episodes and which are ignored.

### Policy pack
A directory containing `SKILL.md`, `ontology.yaml`, `episode_rules.yaml`, `search_recipes.yaml`, and agent instruction files.
