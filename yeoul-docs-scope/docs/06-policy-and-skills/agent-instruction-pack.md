# Agent Instruction Pack

상태: Draft v0.1

## 목적

Agent Instruction Pack은 AI agent가 Yeoul Core를 어떻게 사용할지 설명하는 외부 문서/정책 파일 묶음이다. Core는 AI를 모른다. Agent Pack은 Core를 사용하는 “행동 규칙”만 제공한다.

## 구성

```text
policies/default/
  SKILL.md
  agent_instructions.md
  ontology.yaml
  episode_rules.yaml
  search_recipes.yaml
```

## 역할 분리

### Yeoul Core

- Episode 저장
- Entity 저장
- Fact 저장
- Provenance 저장
- Query/Search 실행
- Fact lifecycle 관리

### Agent Pack

- 언제 기억할지 설명
- 무엇을 기억하지 않을지 설명
- 어떤 ontology를 쓸지 선언
- 어떤 episode rule을 쓸지 선언
- 어떤 search recipe를 우선할지 선언
- agent response에서 memory를 어떻게 사용할지 안내

## `SKILL.md`

Skill file은 agent가 언제 Yeoul을 사용할지 설명한다.

필수 섹션:

- 목적
- 기억 규칙
- 검색 규칙
- 무시 규칙
- provenance 사용 규칙
- 불확실성 처리

예:

```md
# Yeoul Memory Skill

## 목적
장기적으로 유지해야 하는 결정, 선호, 프로젝트 맥락, 상태 변화를 Yeoul에 기록한다.

## 기억 규칙
- 명시적 결정은 episode로 기록한다.
- 담당자, 일정, 설계 변경은 fact로 기록한다.
- 단순 인사나 ack는 기록하지 않는다.

## 검색 규칙
- 사용자가 "전에", "최근", "왜", "누가", "무엇을 결정"이라고 물으면 검색을 먼저 수행한다.
- 검색 결과는 source episode를 확인한 뒤 사용한다.
```

## `agent_instructions.md`

Agent runtime에 주입되는 더 구체적인 지침이다.

포함 내용:

- tool/API 호출 시점
- memory result 사용 방식
- 출처 표기 방식
- contradiction 발견 시 행동
- memory 저장 전 요약 기준

예:

```md
# Agent Instructions for Yeoul

1. Memory search is required when the user asks about prior decisions or historical context.
2. Do not invent memory. If Yeoul returns no result, say that no relevant stored memory was found.
3. When storing memory, preserve the user's wording when it is a decision or constraint.
4. If retrieved facts conflict, surface the conflict instead of choosing silently.
```

## `ontology.yaml`

Domain entity와 predicate를 선언한다.

```yaml
version: 1
entity_types:
  - Person
  - Project
  - Repository
  - File
  - Decision
  - Task
predicates:
  - USES
  - DECIDED
  - OWNS
  - DEPENDS_ON
  - BLOCKED_BY
  - SUPERSEDES
dedup:
  Repository:
    keys: [url]
  File:
    keys: [repository, path]
```

## `episode_rules.yaml`

어떤 입력을 episode로 저장할지 정의한다.

```yaml
version: 1
promote_to_episode:
  - name: explicit_decision
    when:
      contains_any: ["decided", "결정", "합의"]
    priority: high
  - name: architecture_constraint
    when:
      contains_any: ["must", "must not", "해야", "하지 않는다"]
    priority: high
drop:
  - name: low_signal_ack
    when:
      contains_any: ["ok", "확인", "thanks", "감사"]
```

## `search_recipes.yaml`

질문 유형별 검색 전략을 정의한다.

```yaml
version: 1
recipes:
  recent_decisions:
    strategy: fact_search
    filters:
      predicates: [DECIDED, USES, BLOCKED_BY]
      window_days: 90
      status: active
    ranking:
      recency: 0.4
      provenance: 0.3
      confidence: 0.2
      graph_distance: 0.1
```

## Agent behavior patterns

### 기억해야 하는 경우

- 사용자가 명시적 선호를 말함
- 프로젝트 결정이 내려짐
- 설계 방향이 바뀜
- 특정 작업의 owner가 정해짐
- 사용자가 이전 답변을 정정함
- 외부 source에서 중요한 상태 변화가 감지됨

### 검색해야 하는 경우

- 사용자가 과거 내용을 묻는 경우
- 현재 판단이 이전 결정에 의존하는 경우
- 설계 제약을 확인해야 하는 경우
- 같은 이슈가 반복되는지 확인해야 하는 경우

### 기억하지 말아야 하는 경우

- 단순 인사
- 의미 없는 ack
- 일회성 잡담
- 민감 정보 중 policy가 금지한 항목
- 출처 없는 추측

## Contradiction behavior

Agent는 conflicting fact를 발견하면 조용히 하나를 선택하지 않는다.

권장 응답:

```text
저장된 메모리에 충돌이 있습니다. 이전에는 A라고 기록되어 있고, 더 최근에는 B라고 기록되어 있습니다. 최신 기록 기준으로는 B가 active입니다.
```

## Memory citation behavior

Agent가 Yeoul memory를 답변에 사용할 때는 가능하면 source episode나 timestamp를 함께 언급한다.

예:

```text
저장된 프로젝트 메모리 기준으로, 2026-04-17에 Yeoul Core에서 AI agent logic을 제거하기로 결정했습니다.
```

## 금지

Agent Pack은 다음을 포함하면 안 된다.

- DB schema migration
- raw Cypher query
- LLM provider key
- runtime secret
- destructive operation 자동 승인

## 결론

Agent Instruction Pack은 Yeoul의 AI-facing layer이지만, Core가 아니다. 이 분리가 Yeoul을 agent framework가 아니라 reusable memory engine으로 유지한다.
