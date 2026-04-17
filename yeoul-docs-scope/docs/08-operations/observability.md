# Observability

상태: Draft v0.1

## 목적

Yeoul은 local-first library지만, memory engine의 동작을 관찰할 수 있어야 한다. Observability는 디버깅, 성능 튜닝, 검색 품질 개선, 운영 안정성에 필요하다.

## 관찰 대상

- DB open/close
- migration
- episode ingest
- entity resolution
- fact assertion
- fact lifecycle transition
- search/query
- compaction
- backup/restore
- policy validation

## Logging

### 원칙

- 기본 로그는 조용해야 한다.
- destructive operation과 migration은 항상 로그를 남긴다.
- 민감한 content 전체를 로그에 남기지 않는다.
- content hash, ID, count 중심으로 기록한다.

### Log fields

```json
{
  "time": "2026-04-17T12:00:00+09:00",
  "level": "info",
  "component": "ingest",
  "operation": "IngestEpisode",
  "episode_id": "ep_...",
  "source_id": "src_...",
  "duration_ms": 12,
  "status": "success"
}
```

## Metrics

### Storage metrics

- db_open_duration_ms
- query_duration_ms
- transaction_duration_ms
- transaction_failures_total
- db_lock_errors_total

### Ingest metrics

- episodes_ingested_total
- entities_created_total
- entities_matched_total
- facts_created_total
- ingest_failures_total

### Retrieval metrics

- search_requests_total
- search_latency_ms
- search_candidates_count
- search_results_count
- neighborhood_expansion_latency_ms

### Policy metrics

- policy_load_total
- policy_validation_failures_total
- recipe_execution_total

## Tracing

MVP에서는 OpenTelemetry dependency를 필수로 두지 않는다. 대신 hook interface를 제공한다.

```go
type Observer interface {
    OnEvent(event Event)
    OnMetric(metric Metric)
}
```

추후 OpenTelemetry adapter를 제공할 수 있다.

## Search explanation

Observability의 일부로 search explanation을 제공한다.

```json
{
  "strategy": "recent_context",
  "stages": [
    {"name": "candidate_search", "count": 200, "duration_ms": 5},
    {"name": "status_filter", "count": 130, "duration_ms": 1},
    {"name": "ranking", "count": 10, "duration_ms": 2}
  ]
}
```

## Health check

CLI:

```bash
yeoul inspect health
```

Service:

```http
GET /v1/health
```

Health fields:

- schema version
- DB path
- DB mode
- policy loaded
- last migration
- last checkpoint if available

## Debug mode

Debug mode는 다음을 포함한다.

- query plan
- generated Cypher, redacted
- timing
- candidate counts

주의:

- raw content와 secrets는 출력하지 않는다.
- debug mode는 기본 off다.

## 결론

Yeoul observability는 cloud-scale monitoring보다 local debugging과 correctness inspection을 우선한다. 검색 결과의 explainability는 제품 핵심 기능이다.
