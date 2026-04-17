# Transactions

상태: Draft v0.1

## 목적

Yeoul의 ingest, fact lifecycle, entity merge, compaction이 어떤 transaction boundary에서 실행되어야 하는지 정의한다. Temporal graph memory는 여러 node/edge를 함께 갱신하므로 partial write를 허용하면 안 된다.

## 원칙

1. Episode ingest는 atomic해야 한다.
2. Fact assertion은 subject/object/provenance edge와 함께 commit되어야 한다.
3. Supersession은 새 fact 생성과 기존 fact status 변경이 함께 commit되어야 한다.
4. Retraction은 status 변경과 audit metadata가 함께 commit되어야 한다.
5. Compaction apply는 batch 단위 transaction으로 실행한다.

## Transaction boundaries

### IngestEpisode

작업:

1. Source upsert
2. Episode create or dedupe
3. Entity upsert
4. Mentions edge create
5. Fact create
6. Subject/Object edge create
7. Asserts/DerivedFrom edge create

모든 단계가 하나의 transaction 안에서 실행되어야 한다.

### AssertFact

작업:

1. Validate subject entity exists
2. Validate object entity if provided
3. Create Fact
4. Create Subject edge
5. Create Object edge if object exists
6. Create Asserts edge if episode exists

### SupersedeFact

작업:

1. Validate old fact active/uncertain/contradicted
2. Create new fact
3. Set old fact status = superseded
4. Set old fact valid_to if needed
5. Create Supersedes edge
6. Preserve provenance

### RetractFact

작업:

1. Validate fact exists
2. Set status = retracted
3. Set retracted_at
4. Set retraction_reason
5. Write audit metadata

### EntityMerge

작업:

1. Validate source and target entity
2. Create MergedInto edge
3. Set source entity status = merged
4. Reassign or mark affected facts depending policy

MVP에서는 fact reassignment을 자동 수행하지 않고 merge relation만 둔다.

## Isolation assumptions

Ladybug는 serializable ACID transactions를 제공한다고 문서화되어 있다. Yeoul은 storage adapter가 이를 활용하되, 실제 Go API에서 manual transaction과 auto transaction이 어떻게 제공되는지 Phase 0에서 검증해야 한다.

## Auto-transaction vs manual transaction

일부 query execution은 statement마다 transaction이 자동 적용될 수 있다. 하지만 Yeoul ingest는 여러 statement를 묶어야 하므로 manual transaction API가 필요하다.

검증 항목:

- Go binding에서 begin/commit/rollback API 제공 여부
- 실패 시 rollback 동작
- concurrent transaction conflict behavior
- transaction timeout 설정 가능 여부

## Idempotency

Episode ingest는 idempotency를 고려한다.

중복 판단:

- source kind + external ref
- content hash
- idempotency key

Idempotent retry 시:

- 이미 저장된 episode를 반환한다.
- 중복 fact create를 피한다.
- transaction failure 후 retry가 안전해야 한다.

## Batch ingest

Batch ingest는 큰 transaction 하나보다 chunked transaction을 권장한다.

기본:

- 100~1000 episodes per batch
- batch 실패 시 해당 batch rollback
- progress checkpoint 기록

## Error handling

Transaction 실패 시 다음을 반환한다.

- `YEOL_DB_TX_FAILED`
- cause details
- operation name
- rollback status

## Deadlock/conflict behavior

Concurrent write conflict가 발생하면 retry policy를 적용할 수 있다.

```go
type RetryPolicy struct {
    MaxAttempts int
    InitialBackoff time.Duration
    MaxBackoff time.Duration
}
```

MVP 기본:

- read query: no retry or one retry
- write transaction: 3 attempts
- migration: no automatic retry

## 결론

Yeoul의 transaction design은 memory correctness의 핵심이다. Storage adapter는 Ladybug transaction semantics를 숨기고, Core operation 단위로 atomicity를 보장해야 한다.
