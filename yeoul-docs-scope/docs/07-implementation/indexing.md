# Indexing

상태: Draft v0.1

## 목적

Yeoul의 주요 query pattern을 지원하기 위한 index 전략을 정의한다. Ladybug의 구체적인 index syntax와 기능은 Phase 0에서 검증한다.

## Query pattern

MVP에서 중요한 query는 다음이다.

1. Entity fingerprint lookup
2. Entity canonical name search
3. Episode content hash lookup
4. Fact by subject
5. Fact by predicate/status/time
6. Fact provenance lookup
7. Recent facts search
8. Neighborhood expansion

## Index candidates

### Entity

필수:

- `Entity.id` primary key
- `Entity.fingerprint`
- `Entity.type`
- `Entity.namespace`
- `Entity.canonical_name`

목적:

- upsert
- dedup candidate
- entity search

### Episode

필수:

- `Episode.id` primary key
- `Episode.content_hash`
- `Episode.source_id`
- `Episode.group_id`
- `Episode.observed_at`

목적:

- duplicate episode detection
- source-based retrieval
- recent context search

### Fact

필수:

- `Fact.id` primary key
- `Fact.status`
- `Fact.predicate`
- `Fact.observed_at`
- `Fact.valid_from`
- `Fact.valid_to`

목적:

- active fact filtering
- temporal query
- predicate search

### Source

필수:

- `Source.id` primary key
- `Source.kind`
- `Source.external_ref`

목적:

- idempotent source ingestion
- provenance query

## Relationship traversal

Ladybug는 graph DB로서 relationship traversal을 수행한다. Relationship table의 join/index behavior는 Ladybug 내부 구조에 의존한다. Yeoul은 다음 traversal을 자주 사용한다.

- Episode -> Fact via Asserts
- Fact -> Entity via Subject/Object
- Episode -> Entity via Mentions
- Episode -> Source via FromSource
- Entity -> Entity via RelatedTo

## Full-text index

Full-text search는 다음 대상에 유용하다.

- Episode.content
- Entity.canonical_name
- Fact.value_text

MVP에서는 full-text를 optional capability로 둔다.

Capability probe:

```go
func (s *Store) SupportsFullText() bool
```

Fallback:

- simple string contains
- prefix search
- external index sidecar

## Vector index

Vector index는 semantic retrieval을 위한 optional capability다. Core에는 embedding provider가 없다. Vector는 external integration이 넣어줄 수 있다.

가능한 설계:

- `Embedding` node/table 별도 생성
- `target_id`, `target_kind`, `model`, `vector`, `created_at`

MVP에서는 제외한다. v0.4 이후 feature probe로 검토한다.

## Composite index 요구

다음 compound query가 많다.

- active facts by predicate and time
- entity by namespace/type/name
- episode by source/group/time

Ladybug index syntax가 composite index를 지원하면 활용한다. 지원이 제한적이면 single-property filtering + graph traversal로 시작한다.

## Ranking feature dependencies

Ranking에는 다음 값이 필요하다.

- recency: `observed_at`
- confidence: `Fact.confidence`
- provenance: Asserts/Source relationship count
- graph distance: traversal depth
- status: `Fact.status`

따라서 index는 단순 lookup뿐 아니라 ranking candidate generation을 지원해야 한다.

## Index migration

Index 생성은 schema migration의 일부다.

규칙:

- index migration은 idempotent해야 한다.
- index 생성 실패는 schema migration failure로 처리한다.
- optional index 실패는 capability warning으로 처리할 수 있다.

## Observability

Search API에서 `include.explanation`이 true이면 사용된 index/candidate stage를 표시한다.

예:

```json
{
  "query_plan": {
    "candidate_stage": "fact_status_time_filter",
    "used_indexes": ["Fact.status", "Fact.observed_at"],
    "candidate_count": 342
  }
}
```

## 결론

Yeoul의 index 전략은 entity upsert와 temporal fact retrieval을 우선한다. Full-text/vector는 중요하지만 Core MVP의 필수 조건이 아니라 optional retrieval enhancement로 둔다.
