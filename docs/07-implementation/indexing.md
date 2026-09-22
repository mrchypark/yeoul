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

## Derived retrieval runtime

The indexing boundary is a projection layer. Canonical records stay in LatticeDB; projections are derived and rebuildable; search responses hydrate canonical records before provenance or lifecycle-sensitive output is returned.

Yeoul no longer ships or resolves an external retrieval runtime. The projection artifacts exist for inspection and verification via `yeoul index build|rebuild|status|verify`. Projection indexes may be dropped and rebuilt without losing memory truth.

## Benchmark responsibility
Any new index addition should include:
- expected target query
- benchmark before/after
- write amplification consideration
- storage cost note

## LookupFacts endpoint candidates

The initial LatticeDB optimization applies only to current-time `LookupFacts`
requests that name a small subject or object anchor set. It resolves the
persisted `Entity.id` index, follows incoming `SUBJECT` or `OBJECT_ENTITY`
adjacency, and hydrates the returned fact IDs through the normal in-memory
filters. It does not add indexes during lookup or backfill fact properties.

The optimization falls back to the legacy full scan for `as_of` reads,
unsupported or legacy stores, missing readiness metadata, adjacency fanout
above 4096, storage capability errors, or more than 8 expanded raw entity
IDs. The last guard is conservative: canonical alias expansion is currently
O(number of loaded entities), and broad anchors measured slower with native
candidate collection. Candidate results are never limited before filtering,
ordering, or pagination.

The readiness marker is Yeoul-written atomic state, trusted as the writer's
statement that the complete graph projection was established; it is not a
corruption detector or an independent edge-integrity validator. It is written
only for a new empty LatticeDB that is then saved with the complete graph
projection. An existing nonempty database without the marker remains on the
full-scan fallback; an unrelated incremental save cannot claim endpoint-edge
completeness. Read-only lookup never creates or updates the marker.

The native candidate path checks the request context before collection and
between entity/edge/node calls, including while walking a bounded adjacency.
This provides cooperative cancellation between native operations; it is not a
hard wall-clock cancellation guarantee for an individual native call.

Benchmark command:

```sh
go test ./pkg/yeoul -run '^$' -bench BenchmarkLookupFactsCandidateReduction -benchtime=100ms -count=3
```

Repeated sample on Apple M1; values below are medians across three runs:

| Request | 16 entities / 2,000 facts | 140 entities / 300 facts | 1,024 entities / 2,000 facts |
| --- | ---: | ---: | ---: |
| selective subject, indexed | 27,714 ns/op | 65,336 ns/op | 181,234 ns/op |
| selective subject, baseline | 767,894 ns/op | 105,103 ns/op | 659,786 ns/op |
| broad anchors, guarded path | n/a | 313,357 ns/op | 5,067,622 ns/op |
| broad anchors, baseline | n/a | 322,311 ns/op | 5,091,485 ns/op |

The selective path measured about 1.6x at 140 entities and 3.6x at 1,024
entities; the 16-entity fixture measured about 27.7x. These are workload-
specific measurements, not a global performance claim. The 8-request-anchor
guard rejects broad requests before canonical expansion or native collection;
their guarded and baseline medians are correspondingly close.
