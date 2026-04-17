# Local Embedded DB Requirements

This document defines the storage-engine requirements for Yeoul as a local-first memory system.

## Why this document exists
Yeoul depends heavily on the behavior of its storage engine. The engine must support not only graph storage, but also a practical local development and runtime model.

## Functional requirements

### R1. Embedded operation
The database must run in-process and require no external server.

### R2. Durable local storage
The database must support on-disk persistence.

### R3. In-memory mode
The database should support fast ephemeral operation for tests and experiments.

### R4. Graph data model
The database must represent nodes, typed relationships, and node/relationship properties.

### R5. Expressive graph query language
The database should support a graph-native query language suitable for schema creation and retrieval.

### R6. Transactions
The database must provide transactional safety for writes.

### R7. Concurrent connections in one process
The database should support concurrent query execution from multiple connections created from a single owned database object.

### R8. Indexing support
The database should support indexing or equivalent acceleration for Yeoul's hot paths.

## Product-level requirements

### R9. Local-first ergonomics
Starting the database should be as easy as opening a file path.

### R10. Operability
The engine should be easy to ship with a Go binary or local installation.

### R11. Inspectability
Schema and query behavior should remain inspectable by engineers.

### R12. Replay compatibility
The engine should be able to support rebuild workflows from stored episodes.

## Data model requirements

### R13. Stable IDs
The engine should support stable primary identifiers for episodes, entities, facts, and sources.

### R14. Time fields
The engine must support timestamp properties and efficient filtering over time windows.

### R15. Provenance traversal
The engine must support traversals from facts to supporting episodes and sources.

### R16. Historical state
The engine must support active and historical fact retrieval side by side.

## Operational constraints

### C1. Multi-process safety is optional
Yeoul can accept a single-owner local model in MVP, provided that the limitation is documented and daemon mode exists as an escape hatch.

### C2. No mandatory remote dependency
The storage engine must not require a remote cluster or cloud service for the default product path.

### C3. Schema-first is acceptable
A schema-first graph model is acceptable if it improves stability and performance.

## Evaluation questions
- Can the engine survive repeated open/close cycles cleanly?
- Can schema migrations be made deterministic?
- Is the concurrency model compatible with embedded local use?
- Can Yeoul hide raw query language behind a stable API?
- Is the Go integration practical enough for local packaging?

## Current fit assessment
Ladybug is a strong candidate because it matches embedded operation, local persistence, graph structure, and query-language requirements.
Its key constraint is concurrency ownership, which Yeoul must explicitly respect.
