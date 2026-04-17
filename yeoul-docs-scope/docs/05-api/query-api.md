# Query API Specification

상태: Draft v0.1

## 목적

Yeoul의 Query API는 agent나 application이 raw Cypher를 직접 작성하지 않고 memory를 검색할 수 있도록 하는 안정적인 interface다.

## 원칙

1. Raw Cypher는 storage adapter 내부 구현으로 둔다.
2. Query API는 memory semantics를 표현해야 한다.
3. Search result는 provenance와 scoring을 포함할 수 있어야 한다.
4. Temporal query를 기본 지원한다.
5. Historical, active, audit query mode를 구분한다.

## Query 종류

### 1. Text search

자연어 또는 keyword query를 기반으로 관련 episode/fact/entity를 찾는다.

```go
type SearchRequest struct {
    Query string
    Recipe string
    Limit int
    Window *TimeWindow
    Scope *Scope
    Include SearchInclude
    Filters SearchFilters
}
```

### 2. Entity lookup

ID, external ID, fingerprint, canonical name으로 entity를 찾는다.

```go
type EntityLookupRequest struct {
    ID string
    Type string
    Namespace string
    CanonicalName string
    ExternalIDs map[string]string
}
```

### 3. Fact query

subject/predicate/object/time/status 기반으로 fact를 찾는다.

```go
type FactQuery struct {
    SubjectID string
    Predicate string
    ObjectID string
    Status []string
    ValidAt *time.Time
    ObservedWindow *TimeWindow
    Limit int
}
```

### 4. Neighborhood query

특정 entity/fact 주변 graph를 확장한다.

```go
type NeighborhoodRequest struct {
    StartID string
    StartKind string
    Hops int
    EdgeTypes []string
    NodeTypes []string
    Limit int
}
```

### 5. Provenance query

fact의 근거 source/episode를 조회한다.

```go
type ProvenanceRequest struct {
    FactID string
    IncludeEpisodes bool
    IncludeSources bool
    IncludeRawContent bool
}
```

### 6. Contradiction query

새 fact 후보와 충돌할 수 있는 기존 fact를 찾는다.

```go
type ContradictionQuery struct {
    SubjectID string
    Predicate string
    ObjectID string
    ValueText string
    ValidAt *time.Time
}
```

## Search mode

### active

기본 모드. active fact를 우선하고 retracted/superseded는 제외한다.

### historical

특정 시점 또는 기간의 fact를 찾는다. superseded fact도 포함될 수 있다.

### audit

모든 status를 포함한다. retraction, contradiction, supersession history를 확인할 때 사용한다.

```go
type SearchMode string

const (
    SearchModeActive SearchMode = "active"
    SearchModeHistorical SearchMode = "historical"
    SearchModeAudit SearchMode = "audit"
)
```

## Filters

```go
type SearchFilters struct {
    EntityTypes []string
    Predicates []string
    FactStatuses []string
    SourceKinds []string
    GroupID string
    Namespace string
    ValidAt *time.Time
    ObservedAfter *time.Time
    ObservedBefore *time.Time
}
```

## Include options

```go
type SearchInclude struct {
    Entities bool
    Facts bool
    Episodes bool
    Sources bool
    Scoring bool
    Explanation bool
}
```

## Result shape

```go
type SearchResult struct {
    Results []SearchHit
    QueryPlan QueryPlanSummary
    Warnings []Warning
}

type SearchHit struct {
    Kind string
    ID string
    Text string
    Score float64
    Status string
    Entity *Entity
    Fact *Fact
    Episode *Episode
    Source *Source
    Provenance []ProvenanceRef
    ScoreBreakdown map[string]float64
}
```

## Recipe execution

Search recipe는 policy file에서 정의된다.

```yaml
recipes:
  recent_context:
    strategy: hybrid
    filters:
      window_days: 30
      fact_status: active
    ranking:
      recency: 0.4
      graph_distance: 0.2
      confidence: 0.2
      provenance: 0.2
```

Recipe executor는 이를 SearchRequest로 변환한다. Core는 recipe semantics를 제한된 범위에서 실행한다.

## Query plan summary

`--explain`이나 API `include.explanation`이 true일 때 다음을 반환한다.

```json
{
  "strategy": "hybrid",
  "stages": [
    "keyword_candidate_search",
    "fact_status_filter",
    "time_window_filter",
    "graph_expansion",
    "ranking"
  ],
  "candidate_count": 120,
  "returned_count": 10
}
```

## Raw Cypher escape hatch

MVP에서는 raw Cypher API를 제공하지 않는다. 개발자용 debug CLI에서만 restricted mode로 검토한다.

이유:

- Schema drift에 취약하다.
- Agent가 prompt에서 query를 만들면 안전하지 않다.
- Core API contract가 무력화된다.

## 결론

Query API는 Yeoul의 memory semantics를 외부에 드러내는 핵심이다. Storage query language가 Cypher여도, 사용자와 agent는 Yeoul Query API와 Search Recipe를 통해 memory에 접근해야 한다.
