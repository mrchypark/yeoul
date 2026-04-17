# Alternatives

This document records the major alternatives considered for Yeoul storage and architecture.

## Alternative 1: Keep Graphiti directly

### Description
Use Graphiti as-is and adapt around it.

### Pros
- strong existing temporal-memory framing
- fewer greenfield design decisions
- existing conceptual model for episodes, facts, and provenance

### Cons
- Python-first
- agent/framework-oriented
- does not match the desired Go + embedded-core direction
- keeps AI-adjacent behavior closer to the runtime than Yeoul wants

### Verdict
Rejected for core direction mismatch.

## Alternative 2: Neo4j or server-based graph DB

### Description
Use a server-based graph database and connect over network APIs.

### Pros
- mature graph query ecosystem
- good tooling
- known operational model

### Cons
- violates local-first simplicity
- introduces server management
- increases packaging and deployment burden
- weak fit for embedded application workflows

### Verdict
Rejected for MVP because Yeoul should default to embedded local operation.

## Alternative 3: Relational store plus custom graph layer

### Description
Use SQLite, Postgres, or DuckDB tables and build graph semantics manually in the application.

### Pros
- familiar tooling
- potentially simple local storage
- easier packaging in some cases

### Cons
- graph traversal semantics become application code
- Cypher-like graph modeling is lost
- complexity shifts from storage layer to query layer
- provenance and neighborhood queries become more awkward

### Verdict
Rejected for initial implementation because it increases Yeoul code complexity too much.

## Alternative 4: Vector DB plus metadata store

### Description
Store embeddings and metadata, but do not use a graph database.

### Pros
- easy semantic retrieval
- useful for unstructured context
- familiar in AI tooling

### Cons
- poor explicit fact history
- weak provenance graph
- weak relationship traversal
- poor fit for changing structured state over time

### Verdict
Rejected as a primary model. Vector retrieval may be additive, not foundational.

## Alternative 5: Generic document store

### Description
Store episodes and derived facts in JSON documents.

### Pros
- flexible schema
- simple persistence

### Cons
- poor graph traversal
- explicit relationship management becomes application logic
- difficult to maintain clean, queryable temporal structure

### Verdict
Rejected as a primary storage model.

## Alternative 6: Ladybug-backed embedded graph engine

### Description
Use Ladybug as the primary storage engine and build a Go memory layer on top.

### Pros
- embedded
- local-first
- graph-native
- explicit schema
- suitable for temporal, provenance-heavy models

### Cons
- Go binding goes through the C API
- concurrency ownership must be designed carefully
- project-specific abstractions still need to be built

### Verdict
Selected for initial direction.

## Architectural alternative: make skills executable plugins

### Description
Use code plugins instead of YAML/Markdown policy packs.

### Pros
- high flexibility
- can implement arbitrary behavior

### Cons
- behavior becomes harder to inspect
- recompilation or plugin loading complexity
- weak repository readability
- harder to compare policy versions

### Verdict
Rejected for the first product iteration. Policy should be declarative first.
