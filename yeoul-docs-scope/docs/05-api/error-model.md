# Error Model

상태: Draft v0.1

## 목적

Yeoul의 오류를 예측 가능하고 테스트 가능하게 분류한다. CLI, embedded Go API, service API는 같은 error code 체계를 공유해야 한다.

## 원칙

1. 오류는 category와 code를 가진다.
2. Storage engine의 raw error를 그대로 노출하지 않는다.
3. DB lock, migration mismatch, policy validation failure는 별도 code를 가진다.
4. Destructive operation 오류는 recovery hint를 포함한다.
5. Service API는 JSON error object로 반환한다.

## Go error type

```go
type Error struct {
    Code ErrorCode
    Message string
    Details map[string]any
    Cause error
}
```

## Error categories

### Storage errors

- `YEOL_DB_OPEN_FAILED`
- `YEOL_DB_LOCKED`
- `YEOL_DB_QUERY_FAILED`
- `YEOL_DB_TX_FAILED`
- `YEOL_DB_CHECKPOINT_FAILED`
- `YEOL_DB_CORRUPTION_SUSPECTED`

### Schema/migration errors

- `YEOL_SCHEMA_VERSION_MISMATCH`
- `YEOL_MIGRATION_FAILED`
- `YEOL_MIGRATION_DESTRUCTIVE_CONFIRMATION_REQUIRED`
- `YEOL_SCHEMA_VALIDATION_FAILED`

### Input validation errors

- `YEOL_INVALID_EPISODE_INPUT`
- `YEOL_INVALID_ENTITY_INPUT`
- `YEOL_INVALID_FACT_INPUT`
- `YEOL_INVALID_TIME_RANGE`
- `YEOL_INVALID_ID`

### Policy errors

- `YEOL_POLICY_NOT_FOUND`
- `YEOL_POLICY_PARSE_FAILED`
- `YEOL_POLICY_VALIDATION_FAILED`
- `YEOL_RECIPE_NOT_FOUND`
- `YEOL_ONTOLOGY_VALIDATION_FAILED`

### Retrieval errors

- `YEOL_SEARCH_FAILED`
- `YEOL_QUERY_TIMEOUT`
- `YEOL_UNSUPPORTED_QUERY_MODE`
- `YEOL_RANKING_FAILED`

### Lifecycle errors

- `YEOL_FACT_NOT_FOUND`
- `YEOL_FACT_ALREADY_RETRACTED`
- `YEOL_FACT_SUPERSESSION_INVALID`
- `YEOL_ENTITY_MERGE_CONFLICT`

### Permission/local daemon errors

- `YEOL_UNAUTHORIZED`
- `YEOL_FORBIDDEN`
- `YEOL_DAEMON_UNAVAILABLE`
- `YEOL_REQUEST_TOO_LARGE`

## Error shape for service API

```json
{
  "error": {
    "code": "YEOL_POLICY_VALIDATION_FAILED",
    "message": "episode_rules.yaml contains an unknown condition operator.",
    "details": {
      "file": "episode_rules.yaml",
      "path": "promote_to_episode[0].when"
    }
  }
}
```

## CLI exit codes

| Exit code | 의미 |
|---:|---|
| 0 | success |
| 1 | general error |
| 2 | validation error |
| 3 | database error |
| 4 | migration error |
| 5 | policy error |
| 6 | query/search error |
| 7 | destructive confirmation required |
| 8 | lock/concurrency error |

## Recovery hints

오류에는 가능한 경우 recovery hint를 포함한다.

예:

```json
{
  "code": "YEOL_DB_LOCKED",
  "message": "Database is locked by another process.",
  "details": {
    "db_path": "./memory.lbug",
    "hint": "Close the other process or use yeould for multi-process access."
  }
}
```

## Mapping from Ladybug errors

Storage adapter는 Ladybug error를 다음처럼 map한다.

- file lock error → `YEOL_DB_LOCKED`
- query syntax error → `YEOL_DB_QUERY_FAILED`
- transaction conflict → `YEOL_DB_TX_FAILED`
- schema object missing → `YEOL_SCHEMA_VALIDATION_FAILED`

정확한 mapping은 Phase 0 harness에서 실제 error string을 수집한 뒤 보강한다.

## Validation behavior

Input validation은 storage query 전 수행한다.

예:

- missing episode content
- invalid timestamp
- invalid fact status
- unsupported predicate
- invalid policy file path

## Partial failure

Ingest는 transaction boundary 안에서 수행된다. 일부 node/edge만 저장된 상태는 허용하지 않는다. 실패 시 error와 함께 no-op 또는 rollback을 보장해야 한다.

## 결론

Error model은 운영 품질의 일부다. 특히 embedded DB에서는 lock/migration/schema 문제를 사용자가 자주 만날 수 있으므로, 명확한 code와 recovery hint가 필요하다.
