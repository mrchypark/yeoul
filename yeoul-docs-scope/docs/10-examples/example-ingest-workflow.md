# Example Ingest Workflow

상태: Draft v0.1

## 목적

사용자 입력이 Yeoul memory에 저장되는 과정을 예시로 설명한다.

## 입력

```text
Yeoul은 Ladybug를 storage engine으로 사용하고, Go로 다시 구현한다. Core에서는 AI agent 영역을 제거하고, agent 전용 지침은 skill과 instruction 파일로 뺀다.
```

## Step 1. Episode 생성

```json
{
  "kind": "chat_message",
  "content": "Yeoul은 Ladybug를 storage engine으로 사용하고, Go로 다시 구현한다. Core에서는 AI agent 영역을 제거하고, agent 전용 지침은 skill과 instruction 파일로 뺀다.",
  "source": {
    "kind": "chat",
    "external_ref": "thread-yeoul-design-001"
  },
  "group_id": "project:yeoul",
  "observed_at": "2026-04-17T12:00:00+09:00"
}
```

## Step 2. Entity 후보 추출

```json
[
  {"type": "Project", "canonical_name": "Yeoul", "aliases": ["여울"]},
  {"type": "Database", "canonical_name": "Ladybug"},
  {"type": "Language", "canonical_name": "Go"},
  {"type": "Module", "canonical_name": "Core"},
  {"type": "PolicyPack", "canonical_name": "Agent Instruction Pack"},
  {"type": "Skill", "canonical_name": "SKILL.md"},
  {"type": "Document", "canonical_name": "agent_instructions.md"}
]
```

Core는 AI extraction을 수행하지 않는다. 위 후보는 agent adapter, rule extractor, 또는 사용자가 만든 parser가 제공한다.

## Step 3. Entity upsert

각 entity에 대해 fingerprint를 만들고 기존 entity와 매칭한다.

예:

```text
Project:default:yeoul
Database:default:ladybug
Language:default:go
```

## Step 4. Fact 생성

```json
[
  {
    "subject": "Yeoul",
    "predicate": "USES",
    "object": "Ladybug",
    "value_text": "Yeoul uses Ladybug as its storage engine.",
    "confidence": 0.95
  },
  {
    "subject": "Yeoul",
    "predicate": "IMPLEMENTED_IN",
    "object": "Go",
    "value_text": "Yeoul is implemented in Go.",
    "confidence": 0.95
  },
  {
    "subject": "Core",
    "predicate": "EXCLUDES",
    "object": "AI agent logic",
    "value_text": "Yeoul Core excludes AI agent logic.",
    "confidence": 0.9
  },
  {
    "subject": "Agent Instruction Pack",
    "predicate": "DOCUMENTED_IN",
    "object": "SKILL.md",
    "value_text": "Agent-specific instructions are externalized into skill files.",
    "confidence": 0.85
  }
]
```

## Step 5. Provenance 연결

각 fact는 입력 episode와 연결된다.

```text
Episode --ASSERTS--> Fact
Fact --SUBJECT--> Entity
Fact --OBJECT--> Entity
Episode --MENTIONS--> Entity
Episode --FROM_SOURCE--> Source
```

## Step 6. 검색

질문:

```text
Yeoul Core에서 AI agent 로직은 어떻게 처리하기로 했지?
```

검색 요청:

```json
{
  "query": "Yeoul Core AI agent logic",
  "recipe": "recent_decisions",
  "limit": 5,
  "include": {
    "facts": true,
    "episodes": true,
    "sources": true,
    "scoring": true
  }
}
```

예상 결과:

```json
{
  "results": [
    {
      "kind": "fact",
      "text": "Yeoul Core excludes AI agent logic.",
      "status": "active",
      "provenance": {
        "episode": "ep_...",
        "source": "thread-yeoul-design-001"
      }
    }
  ]
}
```

## Step 7. 이후 결정 변경

새 입력:

```text
Core에는 AI agent 로직은 계속 넣지 않지만, Agent Pack 예시는 별도 디렉터리에 포함하자.
```

처리:

- 기존 “Core excludes AI agent logic” fact는 유지한다.
- 새 “Agent Pack examples are included outside Core” fact를 추가한다.
- 상충하지 않으므로 supersede가 아니라 related fact로 추가한다.

## 결론

Yeoul ingest workflow는 episode 중심이다. AI agent가 extraction을 수행할 수는 있지만, Core는 추출된 entity/fact를 검증하고 저장하는 deterministic memory engine으로 남는다.
