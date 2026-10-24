# Yeoul Service API v1

Status: Implemented optional local HTTP adapter (`yeould`)
Last Updated: 2026-10-23

The embedded Go API remains Yeoul's primary boundary. `yeould` exposes a bounded
HTTP/JSON mapping of existing Engine operations for local clients. It does not
expose Cypher, raw Rhiza/Lattice operations, policy execution, or generic admin
commands.

## Transport and security

- HTTP JSON, versioned under `/v1`; default listener `127.0.0.1:8080`.
- The daemon opens an explicitly supplied `--db` path. It creates a database
  only when `--create` is supplied.
- Storage selection is automatic: recognized existing Lattice and Rhiza
  databases keep their current driver and are not converted on open. New
  databases use Rhiza on Linux and macOS, and LatticeDB on Windows; native
  Rhiza storage is not supported on Windows. The HTTP API remains available on
  Windows through LatticeDB.
- Example: `yeould --db "$HOME/.local/share/yeoul/work-memory.ltdb" --create`.
- `--listen` overrides the default address; use `--listen 0.0.0.0:8080` only
  with `YEOUL_HTTP_TOKEN` configured and a trusted TLS/network boundary.
- Loopback binds do not require authentication unless `YEOUL_HTTP_TOKEN` is
  configured. A non-loopback bind requires that environment variable; the
  token is never accepted as a command-line argument.
- Requests on loopback are restricted to loopback/localhost Host values at the
  listener port. An Origin, when present, must be a bare same-host HTTP(S)
  origin. Cross-site Fetch Metadata is rejected. There is no CORS allowance.
- Non-loopback clients must use TLS at a trusted reverse proxy/network boundary;
  `yeould` itself does not configure TLS.
- JSON request bodies are limited to 1 MiB and require
  `Content-Type: application/json`. Unknown fields, duplicate keys, non-object
  roots, and trailing JSON values are rejected. Server read/write timeouts are
  finite.
- `X-Request-ID` accepts a bounded safe token; otherwise the daemon generates
  one. It is returned in both the response header and envelope.
- `Idempotency-Key` is not implemented and is explicitly rejected. Retry
  behavior comes only from the underlying Engine operation and caller-supplied
  stable record IDs; there is no generic replay store.

## Envelope

Every response has `meta`, `data`, and `error`; exactly one of `data` or `error`
is non-null. Query result structs retain their Engine `meta` inside `data`,
while the outer metadata carries the request ID and selected space. Clients
should use `data.meta` for query snapshot and pagination fields.

## API Reference

`GET /docs` serves the Huma OpenAPI reference UI, `GET /openapi.json` serves
the generated OpenAPI 3.1 JSON document, and `GET /openapi.yaml` serves the
same specification as YAML for the reference UI. The specification covers all v1 routes,
request and response envelopes, query parameters, and optional bearer auth.
Both paths use the same Host, Origin, Fetch Metadata, and bearer checks as API
requests. They remain available when the database is not ready. Huma generates
the documentation from the public Yeoul request and response types; live v1
requests continue through the existing strict JSON decoder and handlers.

```json
{
  "meta": {"request_id": "req_123", "space_id": "default"},
  "data": {"meta": {"space_id": "default", "snapshot_at": "2026-10-23T00:00:00Z"}, "hits": []},
  "error": null
}
```

Errors use the same envelope and a stable `error.code`, safe message, and
optional Engine validation details. Storage/internal failures do not expose
backend error details.

## Routes

| Method and path | Engine operation | Success |
| --- | --- | --- |
| `GET /v1/health` | daemon readiness | 200 ready, 503 unavailable |
| `GET /v1/schema` | API version descriptor | 200 |
| `POST /v1/episodes` | `IngestEpisode` | 201 when created, otherwise 200 |
| `PUT /v1/entities/{id}` | `UpsertEntity` | 200 |
| `POST /v1/facts` | `AssertFact` | 201 |
| `POST /v1/facts/{id}/supersede` | `SupersedeFact` | 200 |
| `POST /v1/facts/{id}/retract` | `RetractFact` | 200 |
| `GET /v1/{episodes,entities,facts,sources}/{id}` | `GetRecord` | 200 |
| `POST /v1/query/search` | `Search` | 200 |
| `POST /v1/query/facts` | `LookupFacts` | 200 |
| `POST /v1/query/neighborhood` | `Neighborhood` | 200 |
| `POST /v1/query/timeline` | `Timeline` | 200 |
| `POST /v1/query/provenance` | `Provenance` | 200 |

Query request bodies map to their corresponding public `yeoul` request structs.
They include `meta.space_id` and the fields defined by the Go API. Record GETs
accept `space_id` and `as_of` query parameters. Include-expansion parameters are
not supported and are rejected when requested. IDs containing `/` must be
percent-encoded as `%2F` in a path segment.

Write bodies use the accepted v1 shapes: `{space_id, episode}`,
`{space_id, entity}` for entity upsert, `{space_id, fact}` for assertion,
`{space_id, new_fact, reason}` for supersession, and `{space_id, reason}` for
retraction. Fact status is lifecycle-managed: assertion defaults to `active`,
and state changes use the lifecycle routes. Supersede/retract requests verify
that the target fact belongs to the requested space before mutation.

## Error mapping

- `400`: malformed JSON, invalid arguments, unsupported header.
- `401`: missing or invalid bearer token on authenticated binds.
- `403`: disallowed Host, Origin, or cross-site request.
- `404`: missing record or record outside the requested space.
- `409`: lifecycle conflict, near-duplicate entity, or occupied fact slot.
- `413`: request body over 1 MiB; `415`: non-JSON content type.
- `501`: Engine operation not supported; `503`: storage unavailable.
- `500`: unexpected internal failure.

The adapter does not add metrics, checkpoint, raw query, migration, or entity
resolve endpoints. It does not promise physical read-only mode or durable
request idempotency.
