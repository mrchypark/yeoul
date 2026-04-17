# Example SKILL.md

상태: Draft v0.1

아래는 Yeoul Agent Pack용 `SKILL.md` 예시다.

```md
---
name: yeoul-memory
description: Use Yeoul to store and retrieve long-term temporal graph memory. Core is agent-free; this skill only describes when an agent should call Yeoul memory tools.
version: 1
---

# Yeoul Memory Skill

## 목적

이 skill은 agent가 장기적으로 유지해야 하는 결정, 제약, 프로젝트 맥락, 상태 변화를 Yeoul에 저장하고 검색하기 위한 규칙을 제공한다.

Yeoul Core는 AI agent가 아니다. Agent는 이 skill을 참고하여 Yeoul Core API 또는 Yeoul tool adapter를 호출한다.

## 기억해야 하는 정보

다음은 episode로 기록한다.

- 명시적 결정
- 설계 원칙
- 사용자 선호
- 프로젝트 제약
- 담당자 또는 owner 변경
- 일정, milestone, roadmap 변경
- 반복되는 문제와 해결책
- 사용자가 이전 정보를 정정한 경우

## 기억하지 않을 정보

다음은 기본적으로 기록하지 않는다.

- 단순 인사
- "확인", "감사", "ok" 같은 ack
- 일회성 잡담
- source 없는 추측
- policy가 금지한 민감 정보

## 검색해야 하는 경우

사용자가 다음과 같이 묻는 경우 검색을 먼저 수행한다.

- "전에 뭐라고 했지?"
- "왜 그렇게 결정했지?"
- "최근 결정은?"
- "이 프로젝트의 제약은?"
- "누가 담당하기로 했지?"
- "이전에 같은 문제가 있었나?"

## 검색 결과 사용 규칙

- 검색 결과가 없으면 없다고 말한다.
- 검색 결과가 충돌하면 충돌을 숨기지 않는다.
- retracted fact는 기본 답변 근거로 사용하지 않는다.
- superseded fact는 historical context로만 사용한다.
- 가능하면 source episode의 시점이나 출처를 언급한다.

## 저장 전 요약 규칙

- decision은 사용자의 표현을 최대한 보존한다.
- 긴 대화는 저장 가능한 episode summary로 줄일 수 있다.
- summary는 원문과 다른 새로운 사실을 만들어내면 안 된다.

## 도구 사용 규칙

- 기억 저장: `remember_episode`
- 검색: `search_memory`
- entity 조회: `get_entity`
- fact 철회: 사용자 명시 요청이 있을 때만 `retract_fact`

## 안전 규칙

- 민감 정보는 저장 전에 redaction policy를 확인한다.
- destructive operation은 사용자 확인 없이 실행하지 않는다.
- raw Cypher를 생성하지 않는다.
```
