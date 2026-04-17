# Personas

상태: Draft v0.1

## 목적

이 문서는 Yeoul의 초기 제품 설계에서 누구를 위해 무엇을 만들지 정의한다. Yeoul은 일반 AI agent framework가 아니라, **local-first temporal graph memory substrate**다. 따라서 persona는 “agent 사용자”가 아니라 “장기 기억, 상태 변화, 관계형 맥락을 로컬에서 다루는 개발자와 시스템”을 중심으로 정의한다.

## 핵심 사용자군

### P1. 로컬 AI 도구 개발자

#### 설명

개인용 또는 팀용 AI 도구를 만들고 있다. 도구는 사용자와 장기간 상호작용하며, 과거 결정, 프로젝트 맥락, 선호, 파일, 작업 이력을 기억해야 한다. 하지만 외부 graph database 서버를 운영하고 싶지는 않다.

#### 목표

- 한 개의 로컬 DB 파일에 memory를 저장한다.
- 앱 프로세스 안에서 Go library로 바로 사용한다.
- 대화, 문서, 이슈, 커밋, 작업 기록을 episode로 저장한다.
- 과거 사실이 바뀌었을 때 이전 사실을 삭제하지 않고 이력을 남긴다.
- agent별 기억 규칙을 코드가 아니라 skill/instruction 파일로 바꾼다.

#### 불만

- 기존 vector store는 시간에 따라 바뀌는 사실을 다루기 어렵다.
- 단순 conversation buffer는 provenance가 약하다.
- Neo4j 같은 서버형 graph DB는 개인/로컬 앱에는 무겁다.
- Agent framework에 memory가 묶이면 교체하기 어렵다.

#### 성공 기준

- `yeoul.Open("./memory.lbug")` 같은 형태로 시작할 수 있다.
- 대화나 문서를 넣으면 Episode, Entity, Fact가 저장된다.
- “최근에 이 프로젝트에서 무엇을 결정했나?”를 검색할 수 있다.
- AI SDK 없이도 Core가 작동한다.

---

### P2. Agent harness 개발자

#### 설명

여러 agent를 조립하거나, coding agent/research agent/ops agent용 harness를 직접 만든다. Agent runtime은 별도로 존재하고, memory engine은 독립된 부품으로 쓰고 싶다.

#### 목표

- agent마다 다른 memory policy를 적용한다.
- memory retrieval을 deterministic하게 통제한다.
- prompt 내부에서 raw Cypher를 만들지 않게 한다.
- tool call 결과, issue event, file diff, meeting note를 공통 memory model로 저장한다.
- agent가 바뀌어도 memory DB는 유지한다.

#### 불만

- 많은 agent memory 구현이 특정 LLM provider나 agent framework에 묶여 있다.
- memory capture 규칙이 prompt에 숨어 있으면 테스트가 어렵다.
- 검색 결과가 왜 나왔는지 provenance를 확인하기 어렵다.
- 여러 agent가 공유하는 memory schema가 부재하다.

#### 성공 기준

- Agent runtime은 Yeoul Core를 import하지 않고 service API나 tool adapter로 사용할 수 있다.
- memory policy가 `SKILL.md`, `ontology.yaml`, `episode_rules.yaml`, `search_recipes.yaml`로 분리된다.
- 검색 recipe가 versioned artifact로 관리된다.

---

### P3. 도메인 지식 시스템 개발자

#### 설명

특정 도메인 지식을 점진적으로 쌓아야 하는 시스템을 만든다. 예: 설비 운영, 법률 검토, 연구 노트, 고객 지원, 소프트웨어 프로젝트 지식, 문서 기반 업무 이력.

#### 목표

- 도메인 ontology를 파일로 선언한다.
- domain-specific entity type과 predicate를 정의한다.
- 특정 기간 동안 유효했던 사실을 조회한다.
- 출처가 다른 사실이 충돌할 때 명시적으로 기록한다.
- 시간이 지나며 변경되는 상태를 추적한다.

#### 불만

- 일반 RAG는 “현재 사실”과 “과거 사실”을 혼동한다.
- 업무 시스템마다 데이터 구조가 달라 memory ingestion이 어렵다.
- 중복 entity가 생겨 검색 품질이 떨어진다.
- 변경 이력을 보존하면서 최신 상태를 반환하기 어렵다.

#### 성공 기준

- Ontology와 schema가 분리되지만 일관성을 검증할 수 있다.
- Fact lifecycle이 active, superseded, contradicted, retracted 상태를 지원한다.
- 검색 결과가 Source/Episode까지 추적 가능하다.

---

### P4. 보안/규제 환경의 로컬-first 개발자

#### 설명

데이터를 외부 서비스로 보내기 어렵거나, 로컬 파일로 통제해야 하는 조직/개발자다. AI 기능을 사용할 수는 있지만, memory persistence와 provenance는 로컬에서 통제해야 한다.

#### 목표

- DB 파일 경로를 명확히 통제한다.
- export/delete/retention 정책을 명확히 둔다.
- 민감 정보 redaction hook을 ingestion 전에 적용한다.
- memory 변경 이력과 retraction reason을 남긴다.

#### 불만

- SaaS memory 서비스는 데이터 반출 이슈가 있다.
- Agent가 어떤 정보를 저장했는지 감사하기 어렵다.
- 삭제 요청이나 보존 기간 정책을 memory system이 지원하지 않는다.

#### 성공 기준

- 로컬 파일 단위 백업/복구가 가능하다.
- Retention rule이 episode/fact/source 단위로 적용된다.
- CLI로 memory inspection과 deletion candidate review가 가능하다.

---

### P5. Yeoul 내부 개발자

#### 설명

Yeoul Core와 Agent Pack을 개발하는 엔지니어다. Core가 AI framework로 오염되지 않도록 경계를 지켜야 한다.

#### 목표

- Ladybug storage adapter를 안정화한다.
- Go API를 작고 테스트 가능하게 유지한다.
- policy loader와 core engine의 책임을 분리한다.
- CLI, daemon, agent adapter가 같은 Core API를 사용하게 한다.

#### 성공 기준

- Core package가 LLM SDK를 import하지 않는다.
- Agent Pack은 Core를 import하지 않고 문서/정책 파일로만 존재한다.
- Storage adapter 외부에서 raw Cypher가 거의 보이지 않는다.
- 모든 문서와 테스트가 “agent-free core” 원칙을 검증한다.

## Persona 우선순위

MVP에서는 P1, P2, P5를 우선한다. P3는 schema/ontology 설계에 반영하되, 산업별 완성 템플릿은 후순위다. P4는 early design constraint로 반영하되, 암호화나 identity management 같은 보안 제품 기능은 MVP 범위가 아니다.

## 제품 결정에 미치는 영향

- P1 때문에 embedded Go library가 1순위다.
- P2 때문에 skill/policy file spec이 1순위다.
- P3 때문에 ontology와 Fact lifecycle이 필요하다.
- P4 때문에 local storage, retention, audit 문서가 필요하다.
- P5 때문에 module boundary와 test strategy가 필요하다.
