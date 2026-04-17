# Go API

Status: Accepted  
Last Updated: 2026-04-17

## Purpose

Yeoul's embedded Go API is the **primary application-facing integration surface**.

All other interfaces, including the CLI and optional Service API, must map back to this API rather than inventing a separate memory model. Raw Cypher remains an internal storage concern.

## Design principles

1. **Embedded-first**: the Go API is Yeoul's primary boundary.
2. **Agent-free**: the API does not contain prompts, skills, recipes, tool-calling, or LLM runtime concerns.
3. **Query-contract aligned**: read operations must use the canonical request and response types defined in [query-api.md](/Users/cypark/Documents/project/yeoul/docs/05-api/query-api.md).
4. **Small write surface**: writes expose Yeoul memory operations only.
5. **Context-aware**: all operations accept `context.Context`.
6. **Stable semantics**: API stability matters more than storage-specific convenience.

## Entry point

The embedded API is opened directly in-process.

```go
package yeoul

import "context"

func Open(ctx context.Context, cfg Config) (Engine, error)
```

`Config` owns embedded concerns such as database path, in-memory mode, read-only mode, and create-if-missing behavior.

## Engine interface

```go
package yeoul

import "context"

type Engine interface {
    Close(ctx context.Context) error

    // Write API
    IngestEpisode(ctx context.Context, input EpisodeInput) (*EpisodeResult, error)
    UpsertEntity(ctx context.Context, input EntityInput) (*Entity, error)
    AssertFact(ctx context.Context, input FactInput) (*Fact, error)
    SupersedeFact(ctx context.Context, factID string, input FactInput, reason string) (*SupersedeFactResult, error)
    RetractFact(ctx context.Context, factID string, reason string) (*RetractFactResult, error)

    // Canonical read/query API
    GetRecord(ctx context.Context, req GetRecordRequest) (*GetRecordResponse, error)
    Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
    LookupFacts(ctx context.Context, req FactLookupRequest) (*FactLookupResponse, error)
    Neighborhood(ctx context.Context, req NeighborhoodRequest) (*NeighborhoodResponse, error)
    Timeline(ctx context.Context, req TimelineRequest) (*TimelineResponse, error)
    Provenance(ctx context.Context, req ProvenanceRequest) (*ProvenanceResponse, error)

    // Convenience getters
    GetEpisode(ctx context.Context, id string) (*Episode, error)
    GetEntity(ctx context.Context, id string) (*Entity, error)
    GetFact(ctx context.Context, id string) (*Fact, error)
    GetSource(ctx context.Context, id string) (*Source, error)
}
```

## Read API rule

The embedded read surface must reuse the canonical query models from [query-api.md](/Users/cypark/Documents/project/yeoul/docs/05-api/query-api.md).

This means:

- `Search` maps to `SearchRequest` and `SearchResponse`
- `LookupFacts` maps to `FactLookupRequest` and `FactLookupResponse`
- `Neighborhood` maps to `NeighborhoodRequest` and `NeighborhoodResponse`
- `Timeline` maps to `TimelineRequest` and `TimelineResponse`
- `Provenance` maps to `ProvenanceRequest` and `ProvenanceResponse`
- `GetRecord` maps to `GetRecordRequest` and `GetRecordResponse`

The embedded API is the source of truth. The Service API is only an HTTP transport mapping of these same contracts.

## Convenience getters

`GetEpisode`, `GetEntity`, `GetFact`, and `GetSource` are convenience helpers for common singleton fetches.

They should behave as thin wrappers over `GetRecord` with fixed `kind` values:

- `GetEpisode` -> `kind=episode`
- `GetEntity` -> `kind=entity`
- `GetFact` -> `kind=fact`
- `GetSource` -> `kind=source`

These helpers should not add semantics that do not exist in `GetRecord`.

## Write API scope

The embedded Go API owns Yeoul's write operations:

- episode ingest
- entity upsert
- fact assertion
- fact supersession
- fact retraction

These operations are **not** part of the Query API because the Query API is read-only by design.

## Suggested write result shapes

The exact structs may evolve, but the Go API should make lifecycle transitions explicit.

```go
type EpisodeResult struct {
    EpisodeID string
    SourceID  string
    Created   bool
}

type SupersedeFactResult struct {
    OldFactID string
    NewFactID string
}

type RetractFactResult struct {
    FactID string
    Status string
}
```

## Semantic guarantees

### 1. No raw Cypher

Application code using the public Go API must not need to generate storage queries directly.

### 2. Transport neutrality for reads

A successful embedded read and the equivalent successful service request must have the same logical meaning.

### 3. Temporal correctness

Read methods that accept `TemporalFilter` must obey the temporal rules defined in [query-api.md](/Users/cypark/Documents/project/yeoul/docs/05-api/query-api.md).

### 4. Lifecycle correctness

`SupersedeFact` and `RetractFact` must preserve historical state rather than overwrite or delete it.

### 5. Agent boundary

The embedded API must not accept:

- prompts
- recipes
- skills
- tool calls
- agent instructions

Policy layers may translate those concepts into ordinary Yeoul read and write requests before calling the Go API.

## Error model

The Go API should surface structured errors that align with [error-model.md](/Users/cypark/Documents/project/yeoul/docs/05-api/error-model.md).

At minimum, callers must be able to distinguish:

- invalid argument
- not found
- lifecycle conflict
- unsupported query
- cursor invalidation
- storage failure
- timeout

## Final decisions for v1

1. The embedded Go API is Yeoul's primary product boundary.
2. The canonical read contract is defined by [query-api.md](/Users/cypark/Documents/project/yeoul/docs/05-api/query-api.md).
3. The Service API is a transport adapter over this API, not a second model.
4. Convenience getters are allowed, but they must remain thin wrappers over canonical read semantics.
5. Raw Cypher is not part of the public Go API.
