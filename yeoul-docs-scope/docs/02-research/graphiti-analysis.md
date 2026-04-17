# Graphiti Analysis

상태: Draft v0.1

## 목적

이 문서는 Graphiti에서 Yeoul이 참고할 개념과 버릴 개념을 구분한다. Yeoul은 Graphiti를 그대로 포팅하지 않는다. Yeoul은 Go + Ladybug 기반의 agent-free Temporal Graph Memory Engine이며, Graphiti의 agent-oriented framework 성격은 제거한다.

## Graphiti에서 배울 점

### 1. Temporal knowledge graph

Graphiti의 핵심은 시간이 지남에 따라 변하는 관계와 사실을 knowledge graph로 저장한다는 점이다. 정적 문서 그래프가 아니라, 입력이 계속 들어오고 기존 사실이 바뀔 수 있는 환경을 전제로 한다.

Yeoul에 반영할 점:

- Fact는 시간 필드를 가져야 한다.
- 기존 fact가 바뀌면 overwrite하지 않고 lifecycle transition을 기록해야 한다.
- 검색은 최신 상태와 historical state를 구분해야 한다.

### 2. Episodic processing

Graphiti는 입력을 episode 단위로 처리한다. Episode는 단순 chunk와 다르다. Episode는 관찰된 사건, 메시지, 문서, tool result 같은 provenance-bearing input unit이다.

Yeoul에 반영할 점:

- Episode를 1급 노드로 둔다.
- Entity와 Fact는 episode에서 파생된다.
- Fact는 Source/Episode까지 역추적 가능해야 한다.

### 3. Provenance preservation

Agent memory에서 중요한 문제는 “무엇을 기억하는가”뿐 아니라 “왜 그렇게 기억하는가”다. Graphiti는 data provenance를 강조한다.

Yeoul에 반영할 점:

- 모든 Fact는 최소 하나 이상의 Episode에서 파생되어야 한다.
- Fact에 confidence를 둘 수 있지만, confidence보다 provenance가 더 중요하다.
- Retrieval result는 answer text가 아니라 supporting facts와 source references를 포함해야 한다.

### 4. Hybrid retrieval

Graphiti는 temporal, semantic, full-text, graph search를 결합한다. Yeoul은 MVP에서 semantic/vector search를 필수로 두지 않지만, retrieval strategy를 확장 가능하게 설계해야 한다.

Yeoul에 반영할 점:

- Search recipe abstraction을 둔다.
- Search result에는 scoring breakdown을 포함한다.
- Keyword, graph traversal, recency ranking, provenance ranking을 조합할 수 있어야 한다.

### 5. Custom ontology

Graphiti는 domain-specific entity/edge type을 지원한다. Yeoul도 domain-specific memory를 다룰 수 있어야 한다.

Yeoul에 반영할 점:

- `ontology.yaml`을 제공한다.
- Core schema와 domain ontology를 혼동하지 않는다.
- Core는 최소 schema를 유지하고, domain-level type/predicate는 property와 validation으로 처리한다.

## Graphiti에서 제거할 점

### 1. Agent framework coupling

Yeoul Core는 agent를 실행하지 않는다. Agent runtime, prompt, tool call, LLM orchestration은 Core 밖에 둔다.

제거 대상:

- built-in agent behavior
- built-in LLM call orchestration
- prompt template execution
- autonomous extraction pipeline as core requirement

### 2. Python framework assumption

Graphiti는 Python 생태계와 AI agent framework에 가까운 형태다. Yeoul은 Go library로 설계한다.

제거 대상:

- Python-first API model
- Python-specific async runtime assumption
- Python package as primary product shape

### 3. Backend-specific product assumptions

Graphiti는 여러 graph backend와 AI retrieval layer를 가진다. Yeoul은 Ladybug를 기본 저장소로 선택하고 embedded local-first 경험을 최적화한다.

제거 대상:

- cloud graph DB server as default
- multi-backend abstraction from day one
- remote multi-tenant architecture as MVP

### 4. AI extraction as mandatory path

Yeoul은 AI extraction 없이도 episode/entity/fact를 입력받을 수 있어야 한다. AI가 entity/fact를 추출할 수는 있지만, 이는 Agent Pack 또는 integration layer의 책임이다.

## Yeoul로 재정의하는 핵심 개념

| Graphiti 개념 | Yeoul 대응 | 차이 |
|---|---|---|
| Episode | Episode node | Core input unit으로 유지 |
| Entity | Entity node | domain type은 ontology property로 관리 |
| Fact/relationship | Fact node + relationship edges | Fact를 1급 노드로 둠 |
| Temporal metadata | observed_at, valid_from, valid_to, ingested_at | overwrite 금지 원칙 |
| Provenance | DerivedFrom/Asserts/Source | 모든 fact에서 추적 가능해야 함 |
| Hybrid search | Search recipe | Core는 recipe 실행기, AI 판단 아님 |
| Agent memory | Agent Pack | Core 밖 문서/정책 파일 |

## 구현상 주의

Graphiti와 동일한 feature parity를 목표로 삼으면 범위가 과도해진다. Yeoul의 초기 구현은 다음을 피한다.

- 자동 entity extraction 내장
- automatic ontology learning
- LLM reranking 내장
- community detection 내장
- multi-agent coordination 내장

대신 다음을 우선한다.

- durable temporal graph schema
- provenance correctness
- deterministic ingest/search API
- external policy files
- local embedded operation

## 결론

Yeoul은 Graphiti의 “temporal, episodic, provenance-aware graph memory”라는 핵심 개념을 계승한다. 하지만 agent framework, LLM orchestration, Python runtime, multi-backend complexity는 제거한다. 결과적으로 Yeoul은 Graphiti-Go가 아니라 **agent-free temporal graph memory kernel**이다.

## 참고

- Graphiti overview: https://help.getzep.com/graphiti/getting-started/overview
- Graphiti repository: https://github.com/getzep/graphiti
