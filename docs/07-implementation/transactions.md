# Transactions

Transactions are central to Yeoul correctness because fact lifecycle and provenance must remain consistent.

## Principles
- all write operations should be transactional
- multi-step lifecycle transitions must succeed or fail together
- provenance edges must be created in the same logical write unit as the fact they support

## Write operations requiring a single transaction
- create episode + source link
- create fact + subject/object + provenance edges
- supersede fact + create new fact + lifecycle edge
- retract fact + reason metadata update
- merge entities + redirect references
- compaction plan application

## Suggested transaction patterns

### Pattern 1: simple ingest
1. ensure Source exists
2. create Episode
3. link Episode to Source
4. optionally create or reuse Entities
5. optionally create Facts and provenance edges
6. commit

### Pattern 2: fact supersession
1. lock or otherwise ensure stable target fact context
2. verify target fact exists and is eligible
3. create new active fact
4. create `SUPERSEDES` edge
5. update old fact status to `superseded`
6. commit

### Pattern 3: retraction
1. verify target fact exists
2. update target fact status
3. record reason and actor if present
4. commit

## Idempotency guidance
Some operations should support idempotency keys or content hashes to avoid accidental duplicate ingest.
This is especially important for:
- repeated webhook delivery
- replay jobs
- batch import retry

## Read transactions
Read operations should not require callers to understand the storage engine's internals.
The query layer may still need explicit transaction scoping for consistency-sensitive flows.

## Failure behavior
On transaction failure:
- partial writes must not become visible
- CLI and service adapters should surface the failure with stable error codes
- retry guidance depends on the failure category
