# Observability

Yeoul needs observability even when running locally. Observability is not only for distributed systems; it is also essential for debugging memory correctness.

## Goals
- explain write behavior
- explain retrieval behavior
- surface storage and lifecycle failures
- make policy influence visible

## Observability layers

### Logs
Structured logs should exist for:
- startup and shutdown
- database open/close
- migration runs
- episode ingest
- fact lifecycle changes
- compaction jobs
- policy load and validation
- query execution summaries

### Metrics
Recommended metrics:
- episodes ingested
- facts asserted
- facts superseded
- facts retracted
- query count by type
- query latency by type
- migration duration
- compaction counts
- policy validation failures
- database open failures

### Traces or spans
Even in local mode, internal spans are useful for:
- ingest pipeline timing
- entity resolution timing
- query planning timing
- provenance expansion timing

## Log shape
Suggested fields:
- timestamp
- level
- request_id
- operation
- db_path
- entity_id / fact_id / episode_id when relevant
- duration_ms
- outcome
- error_code if failed

## Query explainability
For selected debug modes, Yeoul should emit:
- chosen recipe
- applied filters
- score components
- number of candidate items before ranking
- truncated explanation path

## Privacy posture
Logs must not dump full sensitive content by default.
Support:
- redacted log mode
- payload length metrics instead of raw content
- explicit debug flags for contentful logs

## Operator tooling
The CLI should provide:
- metrics snapshot
- recent error summary
- migration history
- compaction history
