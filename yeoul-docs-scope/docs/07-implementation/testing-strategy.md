# Testing Strategy

상태: Draft v0.1

## 목적

Yeoul의 테스트 전략을 정의한다. Yeoul은 storage, temporal semantics, provenance, policy validation, CLI, optional daemon이 결합되므로 단순 unit test만으로 충분하지 않다.

## Test pyramid

### Unit tests

대상:

- ID generation
- timestamp handling
- input validation
- policy parsing
- entity fingerprinting
- ranking formula
- error mapping

### Integration tests

대상:

- Ladybug storage adapter
- schema migration
- transaction rollback
- episode ingest
- fact lifecycle
- provenance query

### End-to-end tests

대상:

- CLI workflow
- embedded example
- policy-driven search
- backup/restore
- daemon API if implemented

### Benchmark tests

대상:

- ingest throughput
- search latency
- neighborhood expansion
- concurrent readers/writers

## Storage tests

### Open/close

- on-disk open
- in-memory open
- invalid path error
- read-only mode if supported

### Persistence

1. Create DB
2. Insert source/episode/entity/fact
3. Close DB
4. Reopen DB
5. Verify records and relationships

### Migration

- empty DB migration
- already migrated DB
- version mismatch
- destructive migration confirmation

### Transaction rollback

강제로 중간 실패를 유도한다.

예:

- create episode 성공 후 fact insert 실패
- subject entity missing
- invalid relationship creation

기대:

- partial records must not remain

## Memory model tests

### Fact lifecycle

- create active fact
- supersede fact
- retract fact
- contradiction relation
- historical query

### Provenance

- fact -> episode -> source traversal
- source -> episodes -> facts traversal
- fact without provenance warning

### Entity resolution

- exact key match
- fingerprint match
- alias candidate
- merge relation

## Policy tests

### Valid policy pack

- `SKILL.md` exists
- ontology parses
- episode rules parse
- search recipes parse

### Invalid policy pack

- missing version
- unknown entity type
- unknown predicate
- invalid rule operator
- recipe references missing field

## CLI tests

Use golden output where possible.

```bash
yeoul init --db test.lbug
yeoul ingest episode --text "Core excludes AI agents."
yeoul search "AI agents"
yeoul inspect stats --format json
```

## Concurrency tests

### Single-process

- one Database object
- multiple connections
- 10 readers
- 1 writer
- verify no corruption

### Multi-process negative test

- process A opens DB read-write
- process B attempts read-write
- expect lock error

This test documents expected behavior rather than treating it as product failure.

## Test data

```text
testdata/
  policies/
  episodes/
  sources/
  expected/
```

## CI strategy

MVP CI:

- unit tests
- policy validation tests
- mock storage tests

Nightly CI:

- Ladybug integration tests
- benchmark smoke tests
- concurrency tests

Reason: Ladybug/cgo setup may be heavier than pure Go tests.

## 결론

Yeoul testing은 correctness-first다. 특히 transaction rollback, provenance preservation, fact lifecycle, concurrency assumptions를 자동 테스트로 고정해야 한다.
