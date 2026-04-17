# Service API Specification

상태: Draft v0.1

## 목적

Yeoul은 기본적으로 embedded Go library지만, 여러 process가 같은 memory를 사용해야 할 때 local daemon이 필요하다. Service API는 `yeould`가 제공하는 HTTP/gRPC API의 초안이다.

## 원칙

1. Service API는 optional adapter다.
2. Embedded API와 동일한 semantics를 제공해야 한다.
3. DB file owner는 daemon 하나다.
4. Client process는 Ladybug DB 파일을 직접 열지 않는다.
5. Raw Cypher endpoint는 기본 제공하지 않는다.

## Base URL

```text
http://127.0.0.1:8765
```

## Authentication

MVP local daemon은 bearer token 또는 Unix socket permission을 사용한다.

```http
Authorization: Bearer <local-token>
```

Remote production auth는 non-goal이다.

## API versioning

```text
/v1/...
```

Breaking change는 major version으로 분리한다.

## Endpoints

### Health

```http
GET /v1/health
```

응답:

```json
{
  "status": "ok",
  "schema_version": 1,
  "db_path": "./memory.lbug",
  "mode": "read_write"
}
```

### Ingest episode

```http
POST /v1/episodes
```

요청:

```json
{
  "kind": "chat_message",
  "content": "Yeoul Core must not include agent logic.",
  "source": {
    "kind": "chat",
    "external_ref": "thread_123"
  },
  "observed_at": "2026-04-17T12:00:00+09:00",
  "group_id": "project:yeoul",
  "metadata": {
    "speaker": "user"
  }
}
```

응답:

```json
{
  "episode_id": "ep_01HX...",
  "source_id": "src_01HX...",
  "content_hash": "sha256:...",
  "created": true
}
```

### Upsert entity

```http
POST /v1/entities:upsert
```

요청:

```json
{
  "type": "Project",
  "namespace": "default",
  "canonical_name": "Yeoul",
  "aliases": ["여울"],
  "external_ids": {
    "repo": "github.com/example/yeoul"
  }
}
```

### Assert fact

```http
POST /v1/facts
```

요청:

```json
{
  "subject_id": "ent_...",
  "predicate": "USES_STORAGE",
  "object_id": "ent_...",
  "value_text": "Ladybug",
  "episode_id": "ep_...",
  "confidence": 0.9,
  "valid_from": "2026-04-17T00:00:00+09:00"
}
```

### Search

```http
POST /v1/search
```

요청:

```json
{
  "query": "what did we decide about Ladybug?",
  "recipe": "recent_context",
  "limit": 10,
  "window_days": 30,
  "include": {
    "episodes": true,
    "sources": true,
    "scoring": true
  }
}
```

응답:

```json
{
  "results": [
    {
      "kind": "fact",
      "id": "fact_...",
      "text": "Yeoul uses Ladybug as the storage engine.",
      "score": 0.87,
      "status": "active",
      "provenance": {
        "episode_ids": ["ep_..."],
        "source_ids": ["src_..."]
      },
      "score_breakdown": {
        "recency": 0.4,
        "graph_distance": 0.2,
        "confidence": 0.2,
        "provenance": 0.07
      }
    }
  ]
}
```

### Neighborhood

```http
POST /v1/neighborhood
```

요청:

```json
{
  "entity_id": "ent_...",
  "hops": 2,
  "limit": 100,
  "edge_types": ["SUBJECT", "OBJECT", "MENTIONS", "ASSERTS"]
}
```

### Get entity/fact/episode

```http
GET /v1/entities/{id}
GET /v1/facts/{id}
GET /v1/episodes/{id}
```

### Retract fact

```http
POST /v1/facts/{id}:retract
```

요청:

```json
{
  "reason": "Incorrect extraction from ambiguous source.",
  "retracted_by": "user"
}
```

### Supersede fact

```http
POST /v1/facts/{id}:supersede
```

요청은 새 fact payload를 포함한다.

### Policy validation

```http
POST /v1/policies:validate
```

정책 파일을 업로드하거나 local daemon path를 지정한다. Remote file path access는 보안상 제한한다.

## Error response

```json
{
  "error": {
    "code": "YEOL_DB_LOCKED",
    "message": "Database is locked by another process.",
    "details": {
      "db_path": "./memory.lbug"
    }
  }
}
```

## Idempotency

Ingest endpoint는 optional `idempotency_key`를 지원한다.

```http
Idempotency-Key: sha256:...
```

Episode content hash와 source external ref도 중복 방지에 사용된다.

## 결론

Service API는 embedded mode를 대체하는 것이 아니라 multi-process 상황을 위한 안전한 front door다. Core API와 의미를 맞추고, raw Cypher 노출을 피한다.
