# Yeoul Service API v1

Status: Accepted  
Implementation Status: Deferred optional adapter  
Last Updated: 2026-04-17

## Purpose

This document defines the HTTP service surface for Yeoul when Yeoul runs as an optional local daemon.

The Service API is **not** the primary product boundary. The primary product boundary is the embedded Go API. The Service API exists to support:

- multi-process local access;
- CLI and tooling integration;
- local desktop or workstation deployments; and
- future thin SDKs.

The Service API must remain a strict transport mapping of Yeoul Core semantics.

## Design principles

1. **Optional adapter**: service mode is optional and must not define new memory semantics.
2. **Loopback-first**: v1 targets local access on the same machine.
3. **No raw Cypher endpoint**: the HTTP surface exposes Yeoul operations only.
4. **Stable IDs and JSON**: records are addressed by stable IDs and exchanged as JSON.
5. **Symmetry with embedded mode**: a successful service call must correspond to a valid embedded API call.
6. **Small surface**: only the operations needed for Yeoul v1 are exposed.

## Transport

- Protocol: HTTP/1.1 or HTTP/2
- Content type: `application/json`
- Default bind address: `127.0.0.1`
- Default auth model: no auth in single-user local mode
- TLS: optional, disabled by default in local loopback deployments

## Versioning

The service is versioned in the URL path.

- Current version: `/v1`
- Breaking changes require `/v2`
- Additive response fields are allowed within a version

## Resource model

The Service API exposes four resource categories.

1. **Write resources**
   - episodes
   - entities
   - facts
2. **Read resources**
   - episodes
   - entities
   - facts
   - sources
3. **Query resources**
   - search
   - fact lookup
   - neighborhood
   - timeline
   - provenance
4. **Administrative resources**
   - health
   - metrics
   - schema
   - checkpoints (optional)

## Standard headers

### Request headers

- `Content-Type: application/json`
- `Accept: application/json`
- `X-Request-ID: <opaque>` optional
- `Idempotency-Key: <opaque>` optional for write operations

### Response headers

- `Content-Type: application/json`
- `X-Request-ID: <opaque>` returned if supplied or generated

## Common response envelope

All JSON responses use the following shape.

```json
{
  "meta": {
    "request_id": "req_123",
    "space_id": "default",
    "snapshot_at": "2026-04-17T12:00:00Z",
    "next_cursor": null,
    "total_approx": null
  },
  "data": {},
  "error": null
}
```

### Rules

- Exactly one of `data` or `error` must be non-null.
- `meta.snapshot_at` is included for query endpoints when applicable.
- `meta.next_cursor` is only present for paginated responses.

## Error body

```json
{
  "meta": {
    "request_id": "req_123"
  },
  "data": null,
  "error": {
    "code": "invalid_argument",
    "message": "subject_ids cannot be empty",
    "details": {
      "field": "subject_ids"
    }
  }
}
```

## HTTP status mapping

- `200 OK` successful reads and action-style writes that return a body
- `201 Created` new episode, entity, or fact created
- `202 Accepted` reserved for future asynchronous operations; not used in v1 core flows
- `204 No Content` successful delete-like or retract-like operations without body; Yeoul will prefer `200` with a body in v1
- `400 Bad Request` malformed or invalid arguments
- `404 Not Found` record not found
- `409 Conflict` idempotency conflict, fact lifecycle conflict, or cursor invalidation due to state mismatch
- `422 Unprocessable Entity` semantically valid JSON but invalid domain request
- `429 Too Many Requests` optional for daemon throttling
- `500 Internal Server Error` unexpected server failure
- `503 Service Unavailable` database unavailable or service not ready

## Write endpoints

## POST `/v1/episodes`

Create and ingest an episode.

### Request body

```json
{
  "space_id": "default",
  "episode": {
    "id": "ep_01",
    "kind": "chat_message",
    "content": "We decided to keep raw Cypher internal.",
    "source_id": "src_thread_123",
    "group_id": "project:yeoul",
    "observed_at": "2026-04-17T11:45:00Z",
    "metadata": {
      "author": "user"
    }
  }
}
```

### Response

- `201 Created` when created
- `200 OK` when the request is idempotently replayed and returns the existing record

## PUT `/v1/entities/{id}`

Upsert a canonical entity by stable ID.

### Request body

```json
{
  "space_id": "default",
  "entity": {
    "type": "Project",
    "canonical_name": "Yeoul",
    "aliases": ["여울"],
    "metadata": {
      "repo": "github.com/example/yeoul"
    }
  }
}
```

### Response

- `201 Created` when inserted
- `200 OK` when updated

## POST `/v1/facts`

Assert a new fact.

### Request body

```json
{
  "space_id": "default",
  "fact": {
    "id": "fact_01",
    "predicate": "USES_STORAGE_ENGINE",
    "subject_id": "project:yeoul",
    "object_id": "product:ladybug",
    "confidence": 0.98,
    "status": "active",
    "valid_from": "2026-04-17T00:00:00Z",
    "observed_at": "2026-04-17T11:45:00Z",
    "supporting_episode_ids": ["ep_01"]
  }
}
```

### Response

- `201 Created` when created
- `409 Conflict` when the ID already exists with incompatible contents

## POST `/v1/facts/{id}/supersede`

Create a new fact and link it as superseding an existing fact.

### Request body

```json
{
  "space_id": "default",
  "new_fact": {
    "id": "fact_02",
    "predicate": "USES_STORAGE_ENGINE",
    "subject_id": "project:yeoul",
    "object_id": "product:ladybug",
    "status": "active",
    "valid_from": "2026-05-01T00:00:00Z",
    "observed_at": "2026-05-01T00:00:00Z",
    "supporting_episode_ids": ["ep_22"]
  },
  "reason": "storage decision reaffirmed and normalized"
}
```

### Response

- `200 OK`
- Returns both old and new fact IDs plus lifecycle state

## POST `/v1/facts/{id}/retract`

Retract an existing fact without deleting historical state.

### Request body

```json
{
  "space_id": "default",
  "reason": "source note was incorrect"
}
```

### Response

- `200 OK`

## Read endpoints

## GET `/v1/episodes/{id}`

Fetch an episode by ID.

### Query parameters

- `space_id`
- `as_of` optional
- `include_provenance` optional boolean

## GET `/v1/entities/{id}`

Fetch an entity by ID.

### Query parameters

- `space_id`
- `as_of` optional
- `include_provenance` optional boolean
- `include_related_entities` optional boolean

## GET `/v1/facts/{id}`

Fetch a fact by ID.

### Query parameters

- `space_id`
- `as_of` optional
- `include_provenance` optional boolean
- `include_supporting_episodes` optional boolean

## GET `/v1/sources/{id}`

Fetch a source descriptor by ID.

### Query parameters

- `space_id`

## Query endpoints

These endpoints map directly to the Query API families defined in `query-api.md`.

## POST `/v1/query/search`

Maps to `SearchRequest`.

### Request body

```json
{
  "meta": {
    "space_id": "default"
  },
  "query_text": "why did we remove raw cypher from the public API",
  "mode": "hybrid",
  "scope": {
    "group_ids": ["project:yeoul"]
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

## POST `/v1/query/facts`

Maps to `FactLookupRequest`.

### Request body

```json
{
  "meta": {
    "space_id": "default"
  },
  "subject_ids": ["project:yeoul"],
  "predicates": ["USES_STORAGE_ENGINE"],
  "temporal": {
    "include_inactive": false
  },
  "include": {
    "related_entities": true,
    "provenance": true
  }
}
```

## POST `/v1/query/neighborhood`

Maps to `NeighborhoodRequest`.

### Request body

```json
{
  "meta": {
    "space_id": "default"
  },
  "anchor_ids": ["project:yeoul"],
  "max_hops": 2,
  "node_types": ["Entity", "Fact", "Episode"],
  "edge_types": ["MENTIONS", "ASSERTS", "SUBJECT", "OBJECT", "DERIVED_FROM"]
}
```

## POST `/v1/query/timeline`

Maps to `TimelineRequest`.

### Request body

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

## POST `/v1/query/provenance`

Maps to `ProvenanceRequest`.

### Request body

```json
{
  "meta": {
    "space_id": "default"
  },
  "kind": "fact",
  "id": "fact_01",
  "max_depth": 3
}
```

## Administrative endpoints

## GET `/v1/health`

Returns service liveness and basic database readiness.

### Response example

```json
{
  "meta": {
    "request_id": "req_123"
  },
  "data": {
    "status": "ok",
    "service": "yeould",
    "database": "ready"
  },
  "error": null
}
```

## GET `/v1/schema`

Returns Yeoul schema version and migration status.

## POST `/v1/admin/checkpoint`

Optional endpoint for explicit checkpointing or maintenance hooks.

Not required in embedded mode.

## Service-level rules

### 1. No asynchronous writes in v1

All write endpoints complete synchronously in v1.

### 2. Idempotency

- `POST /v1/episodes` and `POST /v1/facts` should support `Idempotency-Key`.
- `PUT /v1/entities/{id}` is naturally idempotent by resource identity.
- Retraction and supersession endpoints should be idempotent when repeated with the same target state.

### 3. Point-in-time reads

Endpoints that accept `as_of` must evaluate the record or result set as of that timestamp.

### 4. No policy endpoints in core service

The v1 core service does not accept:

- `skill`
- `recipe`
- `agent_instruction`
- `prompt`
- `tool_call`

If a policy layer is added later, it must live in a separate adapter or compatibility service.

### 5. No raw Cypher endpoint

The service will not expose a public `/query/cypher` or `/admin/cypher` endpoint in v1.

### 6. Cursor semantics

Paginated query endpoints return an opaque `meta.next_cursor`. If the underlying state changes and the cursor cannot be resumed safely, the service returns `409 Conflict` with `cursor_invalid`.

## Error codes

The service reuses the logical error codes from the Query API and maps them to HTTP.

Canonical codes:

- `invalid_argument`
- `not_found`
- `already_exists`
- `fact_conflict`
- `cursor_invalid`
- `scope_violation`
- `temporal_conflict`
- `storage_failure`
- `timeout`
- `internal`

## Final decisions for v1

The following are locked for Yeoul v1:

1. The embedded Go API remains primary.
2. The Service API is an optional local adapter.
3. HTTP JSON over loopback is the default service mode.
4. Query endpoints map one-to-one to Query API families.
5. No raw Cypher endpoint is exposed.
6. No policy, skill, or agent concepts appear in the core service contract.
7. All writes are synchronous in v1.
