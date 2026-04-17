# Alternatives

상태: Draft v0.1

## 목적

Yeoul의 저장소와 memory architecture를 선택할 때 검토한 대안을 정리한다. 이 문서는 “왜 Ladybug인가”뿐 아니라 “어떤 경우에 다른 선택이 나은가”를 기록한다.

## 비교 기준

- Embedded/local-first 가능 여부
- Graph query capability
- Temporal fact modeling 적합성
- Go integration
- Operational burden
- Full-text/vector extension 가능성
- Multi-process behavior
- 장기 유지보수 가능성

## 대안 1. Ladybug

### 장점

- Embedded graph database
- Property graph model
- Cypher query language
- On-disk/in-memory 사용 가능
- Go binding 제공
- Full-text/vector index 기능 방향성
- DuckDB-like local analytical DB 감각

### 단점

- Go binding이 C API wrapper라 cgo/build 리스크가 있다.
- 동시성 모델을 제대로 따라야 한다.
- Kuzu successor/fork 계열이라 생태계 안정성은 지속 관찰해야 한다.
- 모든 Graphiti류 기능이 DB만으로 해결되지는 않는다.

### 판단

Yeoul MVP storage engine으로 채택한다. 단, Phase 0에서 build, persistence, concurrency를 반드시 검증한다.

---

## 대안 2. Neo4j

### 장점

- 가장 성숙한 property graph DB 중 하나
- Cypher 생태계가 크다
- tooling과 operational knowledge가 많다
- Graphiti 생태계와도 연관성이 있다

### 단점

- embedded local-first 경험과 맞지 않는다.
- 별도 서버 운영이 필요하다.
- 개인/로컬 agent harness에는 무겁다.
- Yeoul의 “파일 기반 local memory engine” 정체성과 어긋난다.

### 판단

Yeoul Core storage로는 부적절하다. Enterprise/server mode adapter 후보로만 보류한다.

---

## 대안 3. FalkorDB

### 장점

- RedisGraph 계열의 graph query capability
- Graphiti backend로 활용되는 사례가 있다.
- 서버형 graph store로 비교적 가볍다.

### 단점

- embedded local-first가 아니다.
- Redis/FalkorDB server 운영이 필요하다.
- Yeoul의 단일 파일 memory 목표와 다르다.

### 판단

Agent server environment에서는 후보가 될 수 있으나 Yeoul Core MVP에는 맞지 않는다.

---

## 대안 4. SQLite + custom graph tables

### 장점

- 매우 성숙하고 안정적이다.
- Go 생태계가 좋다.
- 파일 기반 local-first 경험이 뛰어나다.
- Multi-process behavior와 tooling이 널리 알려져 있다.

### 단점

- Graph traversal과 Cypher-like query를 직접 구현해야 한다.
- Relationship-heavy query가 복잡해진다.
- Temporal graph memory에 필요한 neighborhood search가 SQL join pile이 된다.
- Full-text는 가능하지만 graph semantics는 별도 구현이다.

### 판단

Fallback storage로 가치가 있다. 하지만 Yeoul의 본질이 graph memory인 만큼 MVP storage로는 Ladybug가 더 적합하다.

---

## 대안 5. DuckDB + custom graph tables

### 장점

- Embedded analytical DB로 성숙하다.
- Columnar analytics, local file, Go integration이 좋다.
- 대량 episode/fact 분석에는 강하다.

### 단점

- Native graph DB가 아니다.
- Graph traversal을 직접 구현해야 한다.
- Temporal fact query는 가능하지만 relationship query가 자연스럽지 않다.

### 판단

Yeoul analytics/export sidecar로는 유용할 수 있다. Core graph store로는 후순위다.

---

## 대안 6. Vector database only

예: Qdrant, Chroma, LanceDB, Milvus Lite 등.

### 장점

- Semantic retrieval에 강하다.
- AI agent memory와 익숙하게 연결된다.
- 유사도 검색 구현이 빠르다.

### 단점

- Temporal fact lifecycle을 잘 표현하지 못한다.
- Entity/relationship/provenance 모델이 약하다.
- “왜 이 기억이 맞는가”를 graph lineage로 추적하기 어렵다.
- 최신/과거 사실 충돌 관리가 어렵다.

### 판단

Optional retrieval index로만 고려한다. Core memory store가 되어서는 안 된다.

---

## 대안 7. 직접 graph engine 구현

### 장점

- 완전한 제어
- Go-native
- cgo 없음
- Yeoul memory model에 최적화 가능

### 단점

- Storage, indexing, transaction, query execution을 직접 구현해야 한다.
- 개발 비용이 매우 크다.
- 초기 제품 목표에서 벗어난다.

### 판단

지금은 금지한다. Ladybug 검증이 실패했을 때만 장기 연구 항목으로 둔다.

## 결론

Yeoul의 핵심 요구는 “embedded local graph memory”다. 이 기준에서 Ladybug가 가장 가까운 후보이며, Neo4j/FalkorDB는 서버형 대안, SQLite/DuckDB는 fallback storage, vector DB는 optional retrieval index로 위치시킨다.
