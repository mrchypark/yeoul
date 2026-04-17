# Example Ontology

상태: Draft v0.1

아래는 Yeoul 프로젝트 개발용 ontology 예시다.

```yaml
version: 1
name: yeoul-project-memory

description: Ontology for tracking product decisions, architecture, implementation tasks, and agent-facing policy documents for Yeoul.

entity_types:
  - Project
  - Module
  - Database
  - Language
  - Decision
  - Document
  - PolicyPack
  - Skill
  - Task
  - Issue
  - Repository
  - File
  - Person

predicates:
  - USES
  - IMPLEMENTED_IN
  - EXCLUDES
  - DECIDED
  - DOCUMENTED_IN
  - DEPENDS_ON
  - BLOCKED_BY
  - SUPERSEDES
  - CONTRADICTS
  - OWNS
  - DEFINES
  - VALIDATES
  - STORES
  - QUERIES

dedup:
  Project:
    namespace: global
    keys: [canonical_name]
  Repository:
    namespace: global
    keys: [url]
  File:
    namespace: repository
    keys: [repository, path]
  Document:
    namespace: repository
    keys: [path]
  Person:
    namespace: organization
    keys: [email, canonical_name]

source_reliability:
  user_instruction: 1.0
  adr: 0.95
  product_doc: 0.9
  issue: 0.7
  chat_message: 0.6
  generated_summary: 0.5

fact_defaults:
  confidence: 0.75
  status: active

retention:
  defaults:
    action: keep
  rules:
    - name: archive_low_signal_chat_after_30_days
      target: Episode
      when:
        kind: chat_message
        metadata.priority: low
        older_than_days: 30
      action: archive
```

## 해설

이 ontology는 Yeoul 제품 개발 자체를 memory에 넣는 경우를 가정한다. Core는 이 ontology를 이용해 predicate와 entity type의 유효성을 검증할 수 있다. 하지만 Core schema가 이 entity type마다 별도 table을 생성하는 것은 아니다. Entity node의 `type` property와 validation으로 처리한다.
