# Roadmap

상태: Draft v0.1

## 원칙

Roadmap은 feature 욕심보다 위험 제거 순서로 구성한다. Yeoul의 가장 큰 초기 리스크는 다음 네 가지다.

1. Ladybug Go binding이 embedded core에 충분히 안정적인가.
2. Temporal graph memory schema가 너무 이르거나 너무 복잡하지 않은가.
3. Agent 영역을 분리한 상태에서도 실제 agent가 사용하기 쉬운가.
4. Local-first storage, backup, retention, inspection이 제품 수준으로 가능한가.

## Phase 0. Storage feasibility

### 목표

Ladybug를 Yeoul의 저장소로 사용할 수 있는지 검증한다.

### 산출물

- Go harness
- Ladybug open/close test
- schema create/drop test
- episode/entity/fact insert test
- restart/reopen persistence test
- single-process concurrent connection test
- multi-process lock behavior test

### 성공 기준

- on-disk DB를 생성하고 재오픈 후 데이터를 조회할 수 있다.
- 단일 Database object에서 여러 Connection이 안전하게 동작한다.
- multi-process conflict가 예측 가능한 error로 드러난다.
- Go binding build process가 문서화된다.

### 제외

- Agent Pack
- Vector search integration
- Compaction
- Production daemon

---

## Phase 1. Core MVP

### 목표

Yeoul Core의 최소 memory model을 구현한다.

### 기능

- `Engine.Open`
- `Engine.Close`
- schema migration
- `IngestEpisode`
- `UpsertEntity`
- `AssertFact`
- `SearchFacts`
- `GetEpisode`
- `GetEntity`
- `GetFact`
- provenance query
- CLI basic commands

### Memory model

- Episode
- Entity
- Fact
- Source
- Mentions
- Asserts
- Subject
- Object
- DerivedFrom

### 성공 기준

- Core package가 AI/LLM dependency 없이 빌드된다.
- 10k episode ingest smoke test를 통과한다.
- fact에서 source episode까지 추적 가능하다.
- CLI로 schema/stats/search를 확인할 수 있다.

---

## Phase 2. Policy and Agent Pack MVP

### 목표

AI agent 전용 규칙을 Core 밖의 파일로 제공한다.

### 기능

- `SKILL.md` template
- `agent_instructions.md` template
- `ontology.yaml` loader
- `episode_rules.yaml` loader
- `search_recipes.yaml` loader
- policy validation
- example agent pack

### 성공 기준

- Core는 policy 없이도 작동한다.
- Policy loader는 invalid config를 명확한 error로 반환한다.
- Agent Pack은 Core package에 import되지 않는다.
- Search recipe를 통해 Core search request가 생성된다.

---

## Phase 3. Retrieval quality

### 목표

단순 키워드/graph traversal을 넘어 memory retrieval 품질을 높인다.

### 기능

- recency-aware ranking
- provenance-aware ranking
- confidence-aware ranking
- neighborhood search
- entity-centric search
- contradiction candidate lookup
- optional full-text integration
- optional vector integration

### 성공 기준

- query type별 recipe가 재현 가능한 결과를 낸다.
- top-k 결과에 scoring breakdown을 제공한다.
- 오래된 fact보다 active/recent fact를 우선한다.
- 단, historical query에서는 과거 fact를 반환할 수 있다.

---

## Phase 4. Reliability and operations

### 목표

로컬 제품으로서 백업, 복구, 관찰, retention을 갖춘다.

### 기능

- backup/restore guide
- database health check
- integrity check
- retention dry-run
- compaction dry-run
- structured logs
- metrics hooks
- benchmark suite

### 성공 기준

- destructive operation은 dry-run을 제공한다.
- fact/episode deletion은 provenance 영향 분석을 출력한다.
- benchmark가 CI 또는 release validation에서 실행 가능하다.

---

## Phase 5. Optional daemon and adapters

### 목표

multi-process 접근이 필요한 경우 로컬 daemon으로 확장한다.

### 기능

- `yeould`
- HTTP API
- gRPC API 후보 검토
- local auth token
- request logging
- connection pool
- MCP adapter optional

### 성공 기준

- 여러 client process가 DB 파일을 직접 열지 않는다.
- daemon만 Ladybug DB owner가 된다.
- embedded API와 service API의 semantics가 일치한다.

## 릴리스 명명

- `v0.1`: Storage feasibility harness
- `v0.2`: Core MVP
- `v0.3`: Policy/Agent Pack MVP
- `v0.4`: Retrieval/ranking
- `v0.5`: Operations/benchmark
- `v0.6`: Optional daemon

## 보류 항목

- Full Graphiti parity
- Built-in LLM extraction
- Built-in autonomous agent runtime
- Hosted cloud service
- Distributed graph DB
- Multi-writer cross-process DB ownership
