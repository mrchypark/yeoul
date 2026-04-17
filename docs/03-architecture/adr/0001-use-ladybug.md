# ADR 0001: Use Ladybug as the storage engine

## Status

Accepted

## Context

Yeoul needs a local-first embedded graph database.

## Decision

Use Ladybug as the initial storage engine.

## Rationale

Ladybug provides:

- embedded in-process execution
- property graph model
- Cypher query language
- on-disk and in-memory modes
- Go API
- full-text and vector retrieval features

## Consequences

- Yeoul must follow Ladybug's schema-first model.
- Yeoul must respect Ladybug's concurrency model.
- Yeoul should avoid exposing raw Cypher as the primary application API.
