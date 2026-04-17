# Yeoul Product Development Documents

작성일: 2026-04-17
상태: Draft v0.1

이 문서 세트는 **Yeoul** 제품 개발을 위한 상세 초안이다. Yeoul은 Go로 작성되는 로컬 우선 **Temporal Graph Memory Engine**이며, 저장소 엔진으로 Ladybug를 사용한다. Core는 AI agent, LLM orchestration, prompt execution, autonomous planning을 포함하지 않는다. AI agent가 Yeoul을 사용할 때 필요한 규칙은 `SKILL.md`, agent instruction, ontology, episode rule, search recipe 같은 외부 파일로 제공한다.

## 범위

이 묶음에는 다음 문서가 포함된다.

### 제품
- `docs/01-product/personas.md`
- `docs/01-product/use-cases.md`
- `docs/01-product/roadmap.md`

### 리서치
- `docs/02-research/graphiti-analysis.md`
- `docs/02-research/local-embedded-db-requirements.md`
- `docs/02-research/alternatives.md`

### 아키텍처
- `docs/03-architecture/plugin-extension-model.md`

### 메모리 모델
- `docs/04-memory-model/provenance.md`
- `docs/04-memory-model/entity-resolution.md`
- `docs/04-memory-model/fact-lifecycle.md`
- `docs/04-memory-model/compaction.md`

### API
- `docs/05-api/cli-spec.md`
- `docs/05-api/service-api.md`
- `docs/05-api/query-api.md`
- `docs/05-api/error-model.md`

### 정책/스킬
- `docs/06-policy-and-skills/agent-instruction-pack.md`

### 구현
- `docs/07-implementation/repo-layout.md`
- `docs/07-implementation/ladybug-cypher-ddl.md`
- `docs/07-implementation/indexing.md`
- `docs/07-implementation/transactions.md`
- `docs/07-implementation/testing-strategy.md`

### 운영
- `docs/08-operations/local-storage.md`
- `docs/08-operations/observability.md`
- `docs/08-operations/performance-benchmarking.md`
- `docs/08-operations/data-retention.md`

### 품질
- `docs/09-quality/test-plan.md`

### 예시
- `docs/10-examples/quickstart.md`
- `docs/10-examples/example-skill.md`
- `docs/10-examples/example-ontology.md`
- `docs/10-examples/example-agent-instructions.md`
- `docs/10-examples/example-ingest-workflow.md`

## 설계 전제

1. Ladybug는 embedded graph database이며 property graph model과 Cypher query language를 제공한다.
2. Ladybug는 on-disk와 in-memory 실행을 지원한다.
3. Yeoul은 기본적으로 local-first embedded Go library로 시작한다.
4. 여러 프로세스가 같은 DB 파일을 동시에 쓰는 구조는 기본 지원 대상으로 두지 않는다.
5. Core는 AI를 모른다. Agent 전용 기능은 Agent Pack 문서와 정책 파일로 분리한다.
6. Raw Cypher는 storage adapter 내부에서 사용하고, 사용자-facing API는 안정적인 Yeoul API로 제공한다.

## 참고 소스

- Ladybug Documentation: https://docs.ladybugdb.com/
- Ladybug GitHub: https://github.com/LadybugDB/ladybug
- Ladybug concurrency: https://docs.ladybugdb.com/concurrency/
- Graphiti Overview: https://help.getzep.com/graphiti/getting-started/overview
- Graphiti GitHub: https://github.com/getzep/graphiti
