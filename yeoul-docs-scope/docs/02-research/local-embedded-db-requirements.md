# Local Embedded DB Requirements

상태: Draft v0.1

## 목적

Yeoul이 사용하는 storage engine이 충족해야 할 요구사항을 정의한다. 이 문서는 Ladybug 채택 여부뿐 아니라, 향후 storage adapter를 바꿀 때의 기준이 된다.

## 기본 제품 요구

Yeoul은 local-first temporal graph memory engine이다. 따라서 storage engine은 다음 조건을 우선해야 한다.

1. 애플리케이션 프로세스 안에 embedded 가능해야 한다.
2. 별도 DB 서버 설치 없이 동작해야 한다.
3. Graph relationship query를 효율적으로 수행해야 한다.
4. 디스크 영속성을 제공해야 한다.
5. Go에서 사용할 수 있어야 한다.
6. Schema와 migration을 관리할 수 있어야 한다.
7. Local backup/restore가 가능해야 한다.

## Functional requirements

### FR-01. Property graph support

Yeoul은 Episode, Entity, Fact, Source를 노드로 두고, Mentions, Asserts, Subject, Object, DerivedFrom 같은 관계를 저장한다. Storage engine은 node/relationship property를 지원해야 한다.

### FR-02. Cypher-like graph query

Yeoul storage adapter는 relationship traversal, neighborhood expansion, fact provenance lookup을 수행해야 한다. Cypher 또는 이에 준하는 graph query language가 필요하다.

### FR-03. On-disk persistence

로컬 memory는 프로세스 재시작 후에도 유지되어야 한다. On-disk DB file을 생성하고, reopen 후 record consistency를 확인할 수 있어야 한다.

### FR-04. In-memory mode

테스트, benchmark, ephemeral agent run을 위해 in-memory mode가 필요하다. 단, in-memory mode는 durable mode가 아니며 production default가 되어서는 안 된다.

### FR-05. Transaction support

Episode ingest는 여러 node/edge insert를 포함한다. 이 작업은 하나의 transaction boundary 안에서 성공 또는 실패해야 한다.

### FR-06. Concurrency within one process

Embedded mode에서 하나의 host process가 여러 goroutine을 통해 read/write query를 수행할 수 있어야 한다. 안전한 사용 방식은 storage engine이 문서화한 concurrency model을 따라야 한다.

### FR-07. Index support

Entity lookup, content hash lookup, fact status filtering, time range filtering을 위해 index가 필요하다. Full-text/vector index는 optional capability로 분류한다.

### FR-08. Export and inspection

CLI 또는 adapter를 통해 schema, record count, selected entities/facts를 inspect할 수 있어야 한다.

## Non-functional requirements

### NFR-01. Local-first

기본 실행은 네트워크 없이 가능해야 한다. Remote service는 optional adapter로만 제공한다.

### NFR-02. Predictable failure

DB lock conflict, migration mismatch, schema error, query timeout은 명확한 error code로 표현되어야 한다.

### NFR-03. Small operational footprint

Yeoul 사용자가 별도 DB server, cluster, background service를 반드시 운영하지 않도록 한다.

### NFR-04. Human-inspectable policy

Storage schema는 binary일 수 있으나, ontology/search policy/skill 파일은 human-readable해야 한다.

### NFR-05. Backup-friendly

DB file과 schema version을 기준으로 백업/복구 절차를 세울 수 있어야 한다.

## Ladybug 적합성 요약

Ladybug는 embedded graph DB이고 property graph model과 Cypher query language를 제공한다. 또한 on-disk/in-memory 사용과 Go binding을 제공한다. Yeoul의 기본 요구사항과 잘 맞는다.

다만 주의해야 할 점은 다음이다.

- Go API는 C API wrapper다. cgo/build/release packaging 리스크가 있다.
- 같은 DB file에 여러 independent Database object가 동시에 read/write하는 구조는 피해야 한다.
- Multi-process access가 필요하면 Yeoul daemon이 DB owner가 되어야 한다.
- Schema-first property graph model을 따르므로 migration system이 필요하다.

## Evaluation checklist

| 항목 | 필수 여부 | 검증 방법 |
|---|---:|---|
| On-disk open/reopen | 필수 | harness test |
| In-memory open | 필수 | unit/smoke test |
| Schema migration | 필수 | migration test |
| Transactional ingest | 필수 | partial failure test |
| Single-process concurrency | 필수 | goroutine stress test |
| Multi-process conflict | 필수 | negative test |
| Full-text search | 선택 | feature probe |
| Vector index | 선택 | feature probe |
| Backup/restore | 필수 | file copy + reopen test |
| CLI inspection | 필수 | end-to-end test |

## 결론

Yeoul은 storage engine을 “SQLite처럼 파일 기반으로 쓰는 graph memory substrate”로 사용하려 한다. Ladybug는 이 방향과 맞지만, concurrency와 Go binding packaging은 초기 검증의 핵심 리스크로 남겨야 한다.
