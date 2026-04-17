# Example Agent Instructions

상태: Draft v0.1

아래는 Yeoul을 사용하는 agent에 주입할 수 있는 지침 예시다.

```md
# Agent Instructions: Yeoul Memory

You may use Yeoul as a temporal graph memory engine. Yeoul Core is not an agent framework. You must treat Yeoul as a storage and retrieval tool only.

## When to search memory

Search Yeoul before answering when the user asks about:

- previous decisions
- project history
- prior constraints
- ownership or responsibility
- why a decision was made
- recent status changes
- repeated issues

## When to store memory

Store an episode when the conversation contains:

- an explicit decision
- a durable user preference
- a project constraint
- a change in architecture
- a task assignment
- a correction to previous memory
- a new source of truth

## How to use retrieved memory

- Do not claim memory exists unless Yeoul returned it.
- Prefer active facts over superseded facts.
- Do not use retracted facts as support.
- If facts conflict, explain the conflict.
- Include time/source context when useful.

## How to create memory

When storing memory:

1. Create an episode with the source and observed time.
2. Extract candidate entities according to ontology.
3. Assert facts with provenance to the episode.
4. Avoid storing low-signal acknowledgements.

## Prohibited behavior

- Do not generate raw Cypher.
- Do not modify schema.
- Do not retract facts unless the user explicitly asks or a policy says so.
- Do not store secrets unless the policy explicitly permits it.
- Do not invent entity IDs.

## Example

User: "Yeoul은 Go로 가고, Core에는 AI agent 로직을 넣지 말자."

Action:
- Store an episode.
- Upsert Project: Yeoul.
- Upsert Language: Go.
- Assert Fact: Yeoul IMPLEMENTED_IN Go.
- Assert Fact: Yeoul EXCLUDES AI agent logic in Core.
```

## 한국어 버전 요약

- 과거 결정/맥락 질문은 먼저 Yeoul 검색.
- 명시적 결정과 장기 제약은 episode로 저장.
- 검색 결과가 없으면 없다고 말함.
- 충돌하는 fact는 숨기지 않음.
- raw Cypher를 만들지 않음.
