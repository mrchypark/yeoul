# Indexing Strategy

This document defines the initial indexing plan for Yeoul.

## Goals
- accelerate hot-path lookups
- keep index count manageable
- prefer correctness and explainability before aggressive tuning

## Hot-path queries
Expected hot paths:
- entity lookup by ID
- entity lookup by canonical or fingerprint-like keys
- fact lookup by ID
- facts by subject + predicate + status
- recent facts in a time window
- episodes by source or group
- provenance traversal from fact to episode/source

## Recommended index priorities

### Priority 0: primary identifiers
- Episode.id
- Entity.id
- Fact.id
- Source.id

### Priority 1: retrieval filters
- Fact.status
- Fact.predicate
- Fact.observed_at
- Episode.observed_at
- Entity.type
- Entity.canonical_name or fingerprint equivalent

### Priority 2: domain-specific or policy-specific keys
- repository URL
- file path
- issue key
- project slug
- source external reference

## Guidance
Do not index every property early.
Measure before expanding index coverage.

## Relationship-driven access
Some Yeoul queries will be dominated by graph traversals rather than scalar property lookups.
These should be optimized through schema shape and query design before adding excessive secondary indexes.

## Full-text search
Where enabled, full-text search should be used for:
- episode content
- entity aliases or names
- source labels

## Vector search
Vector search should remain optional and additive.
It should not replace entity, fact, and provenance-oriented retrieval.

## Benchmark responsibility
Any new index addition should include:
- expected target query
- benchmark before/after
- write amplification consideration
- storage cost note
