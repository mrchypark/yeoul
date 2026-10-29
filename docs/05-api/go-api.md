# Go API

Status: Accepted  
Last Updated: 2026-10-27

## Purpose

Yeoul's embedded Go API is the **primary application-facing integration surface**.

All other interfaces, including the CLI and optional Service API, must map back to this API rather than inventing a separate memory model. Raw Cypher remains an internal storage concern.

## Design principles

1. **Embedded-first**: the Go API is Yeoul's primary boundary.
2. **Agent-free**: the API does not contain prompts, skills, recipes, tool-calling, or LLM runtime concerns.
3. **Query-contract aligned**: read operations must use the canonical request and response types defined in [query-api.md](./query-api.md).
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

Remote recovery is an opt-in for the embedded Rhiza driver only. `Config.RhizaBackup`
accepts a `*RhizaBackupConfig` with `Provider` (`s3`, `gcs`, or `azure`),
`Endpoint`, `Bucket`, nonblank database-unique `Prefix`, `Region`, `Insecure`,
`AccessKey`, `SecretKey`,
`SessionToken`, `GCSServiceAccount`, `AzureStorageAccount`,
`AzureStorageAccountKey`, and `AzureConnectionString`. Yeoul maps these values to
Rhiza; it does not persist credentials. Configuring backup fixes Rhiza local
backup durability to `BeforeAck`; there is no async override.
For S3, `Endpoint` must be `host:port` without a URL scheme; `Insecure` selects
HTTP instead of HTTPS. Azure authentication uses either
`AzureStorageAccountKey` or `AzureConnectionString`, not both.

`RestoreIfMissing` explicitly permits recovery when the local database path is
absent, requires `RhizaBackup`, and is mutually exclusive with
`CreateIfMissing`. Startup fails closed if canonical Yeoul state metadata is
not recovered, rather than initializing an empty database. `CreateIfMissing`
remains the distinct opt-in for first-time database creation. Backup settings
are rejected for LatticeDB, in-memory databases, and unsupported platforms;
they are not consumed by the CLI or HTTP service in this scope. Remote-backed
Rhiza databases require a single writer/owner, including during restoration.
Yeoul does not identify an otherwise valid archive as belonging to a particular
database; callers must use and preserve the correct unique prefix.
Yeoul records a sibling restore-pending marker before remote recovery. A failed
restore leaves it in place, and ordinary opens remain blocked until an explicit
retry successfully validates canonical state. Preserve local artifacts and
retry with the same backup configuration; do not remove the marker to bypass
recovery validation.

Published Yeoul v0.12.0 uses Rhiza v0.20.0; Yeoul v0.13.0 uses Rhiza v0.21.0.
Yeoul v0.14.0 is planned to use Rhiza v0.21.1, whose release adds operating
guidance without changing the v0.21.0 runtime or local archive format. A valid,
quiesced v0.21.0 binding can be reopened with v0.21.1 using the same database
path, identity, and archive configuration; this requires no archive conversion
or fresh archive. Preserve single-writer ownership during the handoff. There
is no automatic migration/adoption from pre-v0.21.0 archive formats. Keep
Yeoul v0.12.0 with its matching database and archive unless a separately
validated migration is available. The
[Rhiza v0.21.1 release contract](https://github.com/mrchypark/rhiza/releases/tag/v0.21.1)
states that unsupported or partial archives are rejected without modification;
Yeoul has no separate archive-format validator, and compatibility with other
existing archives is not guaranteed.
Upgrading the dependency does not convert pre-v0.21.0 archives.

Existing LatticeDB and Rhiza databases are opened by their configured or
detected storage driver; native Ladybug databases must be migrated with a
compatible older release before upgrading.

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
    ResolveEntity(ctx context.Context, req EntityResolveRequest) (*EntityResolveResponse, error)

    // Convenience getters
    GetEpisode(ctx context.Context, id string) (*Episode, error)
    GetEntity(ctx context.Context, id string) (*Entity, error)
    GetFact(ctx context.Context, id string) (*Fact, error)
    GetSource(ctx context.Context, id string) (*Source, error)
}
```

## Read API rule

The embedded read surface must reuse the canonical query models from [query-api.md](./query-api.md).

This means:

- `Search` maps to `SearchRequest` and `SearchResponse`
- `LookupFacts` maps to `FactLookupRequest` and `FactLookupResponse`
- `Neighborhood` maps to `NeighborhoodRequest` and `NeighborhoodResponse`
- `Timeline` maps to `TimelineRequest` and `TimelineResponse`
- `Provenance` maps to `ProvenanceRequest` and `ProvenanceResponse`
- `ResolveEntity` maps to `EntityResolveRequest` and `EntityResolveResponse`
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

## Identity-tuple resolution

`ResolveEntity` resolves an entity by its identity tuple (space, namespace, type, canonical name, stable key)
rather than by ID, because the entity ID is a hash of the exact identity tuple. The response carries
`Matches` (all entities matching the tuple, ordered by ID) and `Drifted` (the subset that only matched
after tolerating namespace/type case drift, empty-vs-populated namespace, or a folded display name that
matches the stored canonical name or an alias).

Set `EntityResolveRequest.IncludeKeyDrift` only for the upsert near-duplicate guard. It widens the
identity rule so a stored entity that carries a stable key also matches a request that carries none
(and the reverse) when the display names overlap, and every such match is reported in `Drifted`.
Ordinary resolution leaves it off, because a stable key is a stronger identity claim than a display
name.

A drifted namespace or type cannot be turned into the canonical ID by recomputation; the derived ID is
computed from the exact tuple provided, so a caller that needs the canonical entity must use the ID
returned in `Matches` rather than recomputing one from the drifted input.

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

Read methods that accept `TemporalFilter` must obey the temporal rules defined in [query-api.md](./query-api.md).

### 4. Lifecycle correctness

`SupersedeFact` and `RetractFact` must preserve historical state rather than overwrite or delete it.
`AssertFact` with `FactInput.Cardinality = "one"` is a single-value slot guard: when an active fact with an overlapping validity interval already occupies the same `space_id + subject_id + predicate` slot, the assert fails with the structured error code `YEOUL_FACT_CONFLICT` (message "single-value fact slot is already occupied"). Nothing is written and nothing is retired. Empty cardinality or `"many"` remains purely additive and append-only. `SupersedeFact` is strictly target-only: it retires exactly the named fact by ID, even when other active facts occupy the same slot. An assertion cannot retire an existing fact. Interval splitting is not implemented.
`UpsertEntity` may use `EntityInput.StableKey` when the caller has a durable identity key; generated IDs should come from namespace, type, and stable key rather than mutable display names.
Generated entity IDs are global across spaces: `EntityID(namespace, type, key)` does not include `SpaceID`, so the same namespace/type/key in two spaces derives one ID and the second upsert fails closed with `YEOUL_LIFECYCLE_INVALID` and an `entity_id` detail instead of creating a space-local entity. Supply a distinct explicit `EntityInput.ID` or a space-qualified namespace when the same identity must exist in more than one space.

### 4.1 Context construction

Context construction is outside the core `Engine` interface. Use `pkg/retrieval.BuildContext(searchResponse, options)` to turn one scoped `SearchResponse` into bounded typed context blocks.

### 5. Agent boundary

The embedded API must not accept:

- prompts
- recipes
- skills
- tool calls
- agent instructions

Policy layers may translate those concepts into ordinary Yeoul read and write requests before calling the Go API.

## Error model

The Go API should surface structured errors that align with [error-model.md](./error-model.md).

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
2. The canonical read contract is defined by [query-api.md](./query-api.md).
3. The Service API is a transport adapter over this API, not a second model.
4. Convenience getters are allowed, but they must remain thin wrappers over canonical read semantics.
5. Raw Cypher is not part of the public Go API.
