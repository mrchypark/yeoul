# Use Cases

상태: Draft v0.1

## 목적

Yeoul이 어떤 상황에서 쓰일 수 있는지 정의한다. 이 문서는 기능 나열이 아니라, 제품 설계와 acceptance criteria를 이끌어내는 사용 시나리오를 기록한다.

## UC-01. 장기 대화 기억 저장

### 설명

사용자와 agent가 반복적으로 대화한다. 대화 중 결정, 선호, 금지 조건, 프로젝트 맥락, 작업 상태가 생긴다. Yeoul은 모든 메시지를 무차별 저장하지 않고, episode rule에 따라 기억 가치가 있는 내용을 episode로 저장한다.

### 입력

- Chat message
- Tool result
- User correction
- Agent summary

### 저장 대상

- Episode: 기억할 만한 대화 단위
- Entity: 사용자, 프로젝트, 파일, 선호, 도구
- Fact: “사용자는 Yeoul Core에서 AI를 제거하기로 했다” 같은 claim
- Source: conversation/session/thread

### 검색 예시

- “지난번에 Yeoul 설계에서 무엇을 제외하기로 했지?”
- “이 사용자가 Go와 Rust 중 무엇을 선호했나?”
- “Ladybug 동시성에 대해 어떤 결론을 냈나?”

### 성공 기준

- 단순 ack는 저장하지 않는다.
- 명시적 결정은 저장한다.
- 검색 결과는 source episode를 포함한다.
- 최신 사실과 과거 사실을 구분한다.

---

## UC-02. 프로젝트 결정 이력 추적

### 설명

소프트웨어 프로젝트를 진행하며 설계 결정이 바뀐다. Yeoul은 decision fact를 시간 정보와 함께 저장하고, 새 결정이 이전 결정을 대체할 때 `SUPERSEDES` 관계를 만든다.

### 입력

- Design meeting note
- Issue comment
- PR description
- Architecture decision record

### 저장 대상

- Entity: Project, Module, Decision, Repository
- Fact: “Yeoul uses Ladybug as storage engine”
- Fact status: active/superseded/retracted

### 검색 예시

- “왜 Ladybug를 선택했나?”
- “이전에 Neo4j를 검토했나?”
- “Agent 영역을 제거한다는 결정의 출처는?”

### 성공 기준

- 결정 변경 시 이전 fact를 삭제하지 않는다.
- 관련 episode와 ADR을 연결한다.
- point-in-time query를 위한 시간 필드를 유지한다.

---

## UC-03. Coding agent memory substrate

### 설명

Coding agent가 repo 내 파일, 이슈, PR, 테스트 실패, 사용자 지침을 기억해야 한다. Yeoul은 coding agent 전용 runtime은 제공하지 않는다. 대신 Agent Pack이 “언제 검색하고 언제 기록할지”를 지침 파일로 제공한다.

### 입력

- File edit summary
- Test result
- User instruction
- GitHub issue/PR event
- Code review comment

### 저장 대상

- Entity: Repository, File, Function, Issue, PullRequest, Test
- Fact: “file X implements storage adapter”
- Relationship: DEPENDS_ON, MODIFIES, FAILS_WITH, FIXED_BY

### 검색 예시

- “이 테스트가 왜 실패했었지?”
- “storage adapter는 어떤 파일에 있나?”
- “이 모듈을 바꿀 때 주의할 점은?”

### 성공 기준

- Core는 GitHub API를 몰라도 된다.
- GitHub adapter는 Source와 Episode를 생성하는 integration layer로만 존재한다.
- 검색은 recipe를 통해 수행한다.

---

## UC-04. 도메인 운영 지식 그래프

### 설명

설비, 고객, 프로젝트, 사건, 문서 등 도메인 지식을 시간축으로 저장한다. Yeoul은 domain ontology를 파일로 받아 Entity와 Predicate를 제한하고, fact conflict를 추적한다.

### 입력

- Incident report
- Field note
- Maintenance log
- Customer request
- Document summary

### 저장 대상

- Entity: Plant, Device, Alarm, WorkOrder, Technician
- Fact: “Device A has alarm B since T”
- Relationship: AFFECTS, ASSIGNED_TO, RESOLVED_BY

### 검색 예시

- “지난 14일 동안 이 설비에서 반복된 알람은?”
- “이 문제를 이전에 누가 해결했나?”
- “가장 최근의 복구 조치는 무엇인가?”

### 성공 기준

- 기간 기반 필터가 기본 검색 조건에 들어간다.
- provenance 없는 fact는 고신뢰 결과로 반환하지 않는다.
- retention rule을 적용할 수 있다.

---

## UC-05. 로컬 메모리 인스펙션과 정리

### 설명

개발자가 CLI로 Yeoul memory를 열어 schema, entity, fact, episode, provenance를 점검한다. Entity 중복 후보를 찾고, 오래된 episode를 compact하거나 삭제 후보로 표시한다.

### 명령 예시

```bash
yeoul inspect schema
yeoul stats
yeoul search "Ladybug concurrency"
yeoul entity list --type Project
yeoul fact get fact_01HX...
yeoul compact --dry-run --scope project:yeoul
```

### 성공 기준

- CLI는 raw Cypher 없이 주요 operation을 제공한다.
- destructive operation은 `--dry-run`과 confirmation을 요구한다.
- output은 human-readable과 JSON mode를 모두 지원한다.

---

## UC-06. Local daemon을 통한 multi-process 접근

### 설명

Ladybug embedded mode는 한 프로세스 안에서 하나의 READ_WRITE database object를 소유하는 방식이 안전하다. 여러 tool이나 agent process가 같은 memory에 접근해야 하면 Yeoul daemon이 DB owner가 되고, 다른 process는 HTTP/gRPC로 접근한다.

### 입력

- 여러 agent process의 ingest/search request
- CLI의 query request
- local UI의 inspect request

### 성공 기준

- DB 파일을 여러 프로세스가 직접 열지 않는다.
- daemon이 connection pool과 transaction boundary를 관리한다.
- embedded mode와 daemon mode가 같은 Core API를 공유한다.

## Use Case 우선순위

MVP는 UC-01, UC-02, UC-05를 우선한다. UC-03은 Agent Pack 예제로 제공한다. UC-06은 동시성 제약이 드러나는 순간 도입할 수 있도록 API 설계를 미리 맞춘다.
