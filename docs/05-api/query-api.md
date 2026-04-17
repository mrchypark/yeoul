# Yeoul Query API v1

Status: Accepted  
Last Updated: 2026-04-17

## Purpose

This document defines the **canonical query model** for Yeoul Core.

Yeoul is a local-first Temporal Graph Memory Engine written in Go and backed by Ladybug. The Query API is the stable, transport-independent surface for reading memory from Yeoul. It applies equally to:

- embedded Go usage;
- a local daemon exposed over HTTP or gRPC; and
- future adapters that need to read Yeoul without using raw Cypher.

The Query API is the semantic contract. The Service API is only an HTTP mapping of this contract.

## Scope

The Query API covers **read operations only**.

Write operations such as episode ingest, entity upsert, fact assertion, fact supersession, and fact retraction are part of the Core write API and the Service write endpoints, but they are not part of the Query API.

## Design goals

1. **Agent-free**: the API must not contain agents, prompts, tool-calling, skills, recipes, or LLM-specific concepts.
2. **Temporal by default**: every query can express time constraints or point-in-time reads.
3. **Provenance-aware**: the caller can request provenance chains without writing custom graph traversals.
4. **Transport-neutral**: the same logical query must work in embedded mode and service mode.
5. **No raw Cypher exposure**: Cypher remains an internal storage concern.
6. **Stable semantics**: the API should remain valid even if the physical schema evolves.

## Non-goals

The Query API is not:

- a general graph query language;
- a public Cypher wrapper;
- an ontology language;
- an agent policy layer;
- a prompt-driven retrieval API; or
- a graph analytics API.

## Canonical query families

Yeoul v1 standardizes six query families.

1. **Record Get**
   - Fetch one Episode, Entity, Fact, or Source by ID.
2. **Context Search**
   - Hybrid retrieval over facts, episodes, and entities using text plus structured constraints.
3. **Fact Lookup**
   - Predicate/subject/object-oriented lookup over assertions.
4. **Neighborhood Query**
   - Graph expansion around one or more anchors.
5. **Timeline Query**
   - Time-ordered retrieval of episodes and fact lifecycle events.
6. **Provenance Query**
   - Trace how a fact or entity view is supported by episodes and derived facts.

These families are deliberately narrow. They are meant to cover the majority of memory use cases without turning Yeoul into a general graph database API.

## Query envelope

All query families share the following conceptual envelope.

```go
type QueryMeta struct {
    RequestID string            `json:"request_id,omitempty"`
    SpaceID   string            `json:"space_id,omitempty"`
    Tags      map[string]string `json:"tags,omitempty"`
}

type ScopeFilter struct {
    GroupIDs    []string `json:"group_ids,omitempty"`
    SourceKinds []string `json:"source_kinds,omitempty"`
    SourceIDs   []string `json:"source_ids,omitempty"`
    EntityTypes []string `json:"entity_types,omitempty"`
    FactStatus  []string `json:"fact_status,omitempty"`
}

type TemporalFilter struct {
    AsOf        *time.Time `json:"as_of,omitempty"`
    ObservedFrom *time.Time `json:"observed_from,omitempty"`
    ObservedTo   *time.Time `json:"observed_to,omitempty"`
    ValidFrom    *time.Time `json:"valid_from,omitempty"`
    ValidTo      *time.Time `json:"valid_to,omitempty"`
    IncludeInactive bool    `json:"include_inactive,omitempty"`
}

type Page struct {
    Limit  int    `json:"limit,omitempty"`
    Cursor string `json:"cursor,omitempty"`
}

type Include struct {
    Provenance      bool `json:"provenance,omitempty"`
    SupportingFacts bool `json:"supporting_facts,omitempty"`
    SupportingEpisodes bool `json:"supporting_episodes,omitempty"`
    RelatedEntities bool `json:"related_entities,omitempty"`
    Snippets        bool `json:"snippets,omitempty"`
}
```

### Envelope rules

- `space_id` partitions multiple logical memory spaces in one Yeoul database. Default: `default`.
- `group_ids` are application-level partitions such as project, thread, workspace, or customer account.
- `as_of` means "evaluate the memory state as of this timestamp".
- `observed_*` filters refer to source-world observation time.
- `valid_*` filters refer to fact validity intervals.
- `include_inactive=false` means the engine should prefer active facts only.
- Pagination must be cursor-based for result sets that are not naturally singleton.

## Shared result metadata

All query responses use the following metadata.

```go
type QueryResponseMeta struct {
    RequestID   string     `json:"request_id,omitempty"`
    SpaceID     string     `json:"space_id,omitempty"`
    SnapshotAt  *time.Time `json:"snapshot_at,omitempty"`
    NextCursor  string     `json:"next_cursor,omitempty"`
    TotalApprox *int64     `json:"total_approx,omitempty"`
}
```

### Metadata rules

- `snapshot_at` records the timestamp at which Yeoul evaluated the query.
- `total_approx` is optional and only returned when it can be produced cheaply.
- `next_cursor` is opaque and must be treated as uninterpretable by clients.

## Family 1: Record Get

### Goal

Fetch a single record by stable ID.

### Request

```go
type GetRecordRequest struct {
    Meta     QueryMeta      `json:"meta,omitempty"`
    Kind     string         `json:"kind"` // episode | entity | fact | source
    ID       string         `json:"id"`
    Temporal TemporalFilter `json:"temporal,omitempty"`
    Include  Include        `json:"include,omitempty"`
}
```

### Response

```go
type GetRecordResponse struct {
    Meta   QueryResponseMeta `json:"meta"`
    Kind   string            `json:"kind"`
    Record any               `json:"record"`
}
```

### Semantics

- `kind=episode` returns the stored episode payload and metadata.
- `kind=entity` returns the canonical entity view.
- `kind=fact` returns the fact plus lifecycle metadata.
- `kind=source` returns the source descriptor.
- When `temporal.as_of` is set for `entity` or `fact`, Yeoul should return the representation visible at that time.

## Family 2: Context Search

### Goal

Retrieve relevant memory context using hybrid matching over text, facts, and graph structure.

### Request

```go
type SearchMode string

const (
    SearchModeHybrid   SearchMode = "hybrid"
    SearchModeKeyword  SearchMode = "keyword"
    SearchModeSemantic SearchMode = "semantic"
)

type SearchRequest struct {
    Meta        QueryMeta       `json:"meta,omitempty"`
    QueryText   string          `json:"query_text"`
    Mode        SearchMode      `json:"mode,omitempty"`
    Scope       ScopeFilter     `json:"scope,omitempty"`
    Temporal    TemporalFilter  `json:"temporal,omitempty"`
    AnchorIDs   []string        `json:"anchor_ids,omitempty"`
    Predicates  []string        `json:"predicates,omitempty"`
    Types       []string        `json:"types,omitempty"` // fact | episode | entity
    MinScore    *float64        `json:"min_score,omitempty"`
    Include     Include         `json:"include,omitempty"`
    Page        Page            `json:"page,omitempty"`
}
```

### Response

```go
type SearchHit struct {
    HitID       string   `json:"hit_id"`
    HitType     string   `json:"hit_type"` // fact | episode | entity
    RecordID    string   `json:"record_id"`
    Score       float64  `json:"score"`
    MatchedText string   `json:"matched_text,omitempty"`
    Reasons     []string `json:"reasons,omitempty"`
}

type SearchResponse struct {
    Meta     QueryResponseMeta `json:"meta"`
    Hits     []SearchHit       `json:"hits"`
    Included IncludedRecords   `json:"included,omitempty"`
}

type IncludedRecords struct {
    Episodes []Episode `json:"episodes,omitempty"`
    Entities []Entity  `json:"entities,omitempty"`
    Facts    []Fact    `json:"facts,omitempty"`
    Sources  []Source  `json:"sources,omitempty"`
}
```

### Semantics

- `mode=hybrid` is the default and may combine keyword, vector, and graph-aware reranking.
- `anchor_ids` bias results toward graph-local context without exposing raw traversal details.
- `types` restrict the result set; default is `fact, episode, entity`.
- `predicates` act as a soft filter when searching facts.
- When `include.snippets=true`, returned records may include short matched excerpts.

## Family 3: Fact Lookup

### Goal

Perform structured retrieval of facts without free-text search.

### Request

```go
type FactLookupRequest struct {
    Meta        QueryMeta      `json:"meta,omitempty"`
    Scope       ScopeFilter    `json:"scope,omitempty"`
    Temporal    TemporalFilter `json:"temporal,omitempty"`
    SubjectIDs  []string       `json:"subject_ids,omitempty"`
    Predicates  []string       `json:"predicates,omitempty"`
    ObjectIDs   []string       `json:"object_ids,omitempty"`
    ObjectText  string         `json:"object_text,omitempty"`
    Include     Include        `json:"include,omitempty"`
    Page        Page           `json:"page,omitempty"`
}
```

### Response

```go
type FactLookupResponse struct {
    Meta     QueryResponseMeta `json:"meta"`
    Facts    []Fact            `json:"facts"`
    Included IncludedRecords   `json:"included,omitempty"`
}
```

### Semantics

- Omitted `subject_ids`, `predicates`, or `object_ids` widen the match.
- `object_text` is for literal or text-valued facts.
- This query family is the primary way to answer questions such as:
  - "what projects is this person working on?"
  - "what is the current owner of this task?"
  - "what facts about repository X were active last month?"

## Family 4: Neighborhood Query

### Goal

Traverse the graph locally around one or more anchors.

### Request

```go
type NeighborhoodRequest struct {
    Meta            QueryMeta      `json:"meta,omitempty"`
    Scope           ScopeFilter    `json:"scope,omitempty"`
    Temporal        TemporalFilter `json:"temporal,omitempty"`
    AnchorIDs       []string       `json:"anchor_ids"`
    MaxHops         int            `json:"max_hops,omitempty"`
    EdgeTypes       []string       `json:"edge_types,omitempty"`
    NodeTypes       []string       `json:"node_types,omitempty"`
    MaxNodes        int            `json:"max_nodes,omitempty"`
    Include         Include        `json:"include,omitempty"`
}
```

### Response

```go
type GraphNode struct {
    ID    string         `json:"id"`
    Type  string         `json:"type"`
    Label string         `json:"label,omitempty"`
    Meta  map[string]any `json:"meta,omitempty"`
}

type GraphEdge struct {
    ID     string         `json:"id"`
    Type   string         `json:"type"`
    FromID string         `json:"from_id"`
    ToID   string         `json:"to_id"`
    Meta   map[string]any `json:"meta,omitempty"`
}

type NeighborhoodResponse struct {
    Meta  QueryResponseMeta `json:"meta"`
    Nodes []GraphNode       `json:"nodes"`
    Edges []GraphEdge       `json:"edges"`
}
```

### Semantics

- `max_hops` defaults to `1`.
- `max_hops` must be capped by the implementation to prevent explosive expansions.
- `edge_types` and `node_types` are allow-lists.
- Neighborhood is a graph context API, not a graph analytics API.

## Family 5: Timeline Query

### Goal

Read memory as ordered events over time.

### Request

```go
type TimelineRequest struct {
    Meta        QueryMeta      `json:"meta,omitempty"`
    Scope       ScopeFilter    `json:"scope,omitempty"`
    AnchorIDs   []string       `json:"anchor_ids,omitempty"`
    EventTypes  []string       `json:"event_types,omitempty"` // episode | fact_created | fact_superseded | fact_retracted
    Temporal    TemporalFilter `json:"temporal,omitempty"`
    Descending  bool           `json:"descending,omitempty"`
    Page        Page           `json:"page,omitempty"`
}
```

### Response

```go
type TimelineEvent struct {
    EventID    string         `json:"event_id"`
    EventType  string         `json:"event_type"`
    RecordType string         `json:"record_type"`
    RecordID   string         `json:"record_id"`
    Timestamp  time.Time      `json:"timestamp"`
    Summary    string         `json:"summary,omitempty"`
    Meta       map[string]any `json:"meta,omitempty"`
}

type TimelineResponse struct {
    Meta   QueryResponseMeta `json:"meta"`
    Events []TimelineEvent   `json:"events"`
}
```

### Semantics

- Timeline is sorted by timestamp, ascending by default.
- For facts, timeline events may represent creation, supersession, contradiction, retraction, and expiration.
- `anchor_ids` restrict the timeline to events involving specific records.

## Family 6: Provenance Query

### Goal

Expose support chains and derivation paths.

### Request

```go
type ProvenanceRequest struct {
    Meta        QueryMeta      `json:"meta,omitempty"`
    Kind        string         `json:"kind"` // entity | fact | episode
    ID          string         `json:"id"`
    Temporal    TemporalFilter `json:"temporal,omitempty"`
    MaxDepth    int            `json:"max_depth,omitempty"`
}
```

### Response

```go
type ProvenanceNode struct {
    ID    string         `json:"id"`
    Type  string         `json:"type"`
    Label string         `json:"label,omitempty"`
    Meta  map[string]any `json:"meta,omitempty"`
}

type ProvenanceEdge struct {
    ID     string `json:"id"`
    Type   string `json:"type"`
    FromID string `json:"from_id"`
    ToID   string `json:"to_id"`
}

type ProvenanceResponse struct {
    Meta  QueryResponseMeta `json:"meta"`
    Root  ProvenanceNode    `json:"root"`
    Nodes []ProvenanceNode  `json:"nodes"`
    Edges []ProvenanceEdge  `json:"edges"`
}
```

### Semantics

- Provenance follows `DERIVED_FROM`, `ASSERTS`, `SUPPORTS`, `SUPERSEDES`, and equivalent internal edges.
- `max_depth` defaults to `2` and must be capped.
- Provenance is for explanation and inspection, not arbitrary graph traversal.

## Temporal semantics

All query families inherit Yeoul temporal semantics.

### Rules

1. `as_of` is a point-in-time read.
2. If `as_of` is set, `include_inactive=false` means only records active at that time are visible.
3. `observed_from`/`observed_to` filter by when information was observed in the source world.
4. `valid_from`/`valid_to` filter by the fact validity interval.
5. Timeline queries may ignore `as_of` when the caller explicitly wants historical events over a range.

## Ranking semantics

Ranking applies only to `Context Search`.

### Rules

1. Scores are implementation-defined floating-point numbers.
2. Scores are only comparable within one response.
3. The engine may combine keyword relevance, semantic similarity, recency, graph distance, and confidence.
4. The API does not expose ranking weights directly in v1.
5. Policy layers may translate recipes into explicit filters and anchors before calling the Query API.

## Pagination

### Rules

- Cursor-based pagination is required for `Search`, `Fact Lookup`, and `Timeline`.
- Cursor format is opaque.
- Replaying a cursor against a materially different database state may return `cursor_invalid`.

## Determinism and consistency

- In embedded mode, queries execute against the current process-visible database state.
- In service mode, `snapshot_at` represents the service-side evaluation snapshot.
- The Query API does not promise stable ordering unless documented for a family.
- Timeline queries promise time ordering.

## Query API errors

The Query API uses logical error codes, independent of transport.

Canonical query error codes:

- `invalid_argument`
- `not_found`
- `unsupported_query`
- `cursor_invalid`
- `temporal_conflict`
- `scope_violation`
- `storage_failure`
- `timeout`
- `internal`

## Examples

### Example 1: Hybrid search for recent context

```json
{
  "meta": {
    "space_id": "default"
  },
  "query_text": "what changed about Ladybug concurrency",
  "mode": "hybrid",
  "scope": {
    "group_ids": ["project:yeoul"],
    "entity_types": ["Document", "Decision", "Repository"]
  },
  "temporal": {
    "observed_from": "2026-03-01T00:00:00Z",
    "include_inactive": false
  },
  "types": ["fact", "episode"],
  "include": {
    "provenance": true,
    "supporting_episodes": true,
    "snippets": true
  },
  "page": {
    "limit": 20
  }
}
```

### Example 2: Current owner lookup

```json
{
  "meta": {
    "space_id": "default"
  },
  "subject_ids": ["task:api-spec-finalization"],
  "predicates": ["OWNED_BY"],
  "temporal": {
    "include_inactive": false
  },
  "include": {
    "related_entities": true,
    "provenance": true
  }
}
```

### Example 3: Timeline for a project

```json
{
  "meta": {
    "space_id": "default"
  },
  "anchor_ids": ["project:yeoul"],
  "event_types": ["episode", "fact_created", "fact_superseded"],
  "temporal": {
    "observed_from": "2026-01-01T00:00:00Z",
    "observed_to": "2026-04-17T00:00:00Z"
  },
  "descending": true,
  "page": {
    "limit": 50
  }
}
```

## Final decisions for v1

The following are locked for Yeoul v1:

1. The Query API is the canonical read contract.
2. Raw Cypher is not part of the public API.
3. Query families are explicit and finite.
4. Time and provenance are first-class.
5. Policy files and skills are not accepted as query inputs by Core.
6. Service mode must be a strict mapping of this Query API.
