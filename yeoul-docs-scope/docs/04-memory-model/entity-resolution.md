# Entity Resolution

상태: Draft v0.1

## 목적

Entity resolution은 같은 대상을 여러 이름으로 저장하지 않도록 하는 과정이다. Yeoul은 AI extraction을 Core에 내장하지 않지만, 들어온 EntityInput을 안정적으로 정규화하고 중복 후보를 관리해야 한다.

## 원칙

1. Entity resolution은 완전 자동 병합보다 안전한 후보 제안을 우선한다.
2. Core는 domain ontology를 참조할 수 있지만, LLM 판단은 하지 않는다.
3. Entity merge는 되돌릴 수 있어야 한다.
4. Entity alias는 provenance를 가져야 한다.
5. 같은 문자열이 항상 같은 entity를 의미한다고 가정하지 않는다.

## Entity 필드

```text
id
namespace
type
canonical_name
aliases_json
fingerprint
created_at
updated_at
metadata_json
```

### namespace

같은 이름이 다른 공간에서 다른 entity를 의미할 수 있다.

예:

- repository별 file path
- organization별 project name
- tenant별 user name

### type

Ontology에서 정의한 domain entity type이다.

예:

- Person
- Project
- Repository
- File
- Decision
- Issue

### canonical_name

검색과 표시에서 기본으로 사용할 이름이다.

### aliases_json

다른 표기, 약어, 이전 이름을 저장한다.

### fingerprint

중복 탐색을 위한 deterministic key다.

예:

```text
lowercase(type + ':' + namespace + ':' + normalized_name)
```

## Resolution strategy

### 1. Exact key match

가장 안전한 방식이다.

예:

- email
- repository URL
- file path + repository ID
- issue number + repository ID
- explicit external ID

정책 예:

```yaml
dedup:
  Person:
    keys: [email]
  Repository:
    keys: [url]
  File:
    keys: [repository_id, path]
```

### 2. Canonical name match

정규화된 이름이 같으면 같은 entity 후보로 본다.

정규화:

- trim
- lowercase
- Unicode normalization
- whitespace collapse
- punctuation normalization

주의:

- 이름만으로 자동 병합하지 않는다.
- same namespace + same type일 때만 자동 병합을 고려한다.

### 3. Alias match

입력 이름이 기존 alias와 일치하면 후보로 본다.

주의:

- alias는 ambiguous할 수 있다.
- 여러 entity가 같은 alias를 가질 수 있다.

### 4. Similarity candidate

문자열 유사도, embedding similarity, graph context similarity로 후보를 만들 수 있다. MVP에서는 자동 병합하지 않고 candidate만 기록한다.

## Merge model

Entity merge는 destructive update가 아니라 relationship으로 표현할 수 있다.

- `(:Entity)-[:MERGED_INTO]->(:Entity)`
- source entity status: `merged`
- target entity remains active

Merge metadata:

- `merged_at`
- `merged_by`
- `reason`
- `confidence`

## Split model

잘못 병합된 entity를 분리해야 할 수 있다.

Split은 복잡하므로 MVP에서는 다음을 제공한다.

- merged entity relation 제거 또는 비활성화
- affected facts review list 출력
- manual reassignment 지원

## Entity resolution API

```go
type EntityInput struct {
    Type string
    Namespace string
    CanonicalName string
    Aliases []string
    ExternalIDs map[string]string
    Metadata map[string]any
}

type EntityResolutionResult struct {
    Entity Entity
    Created bool
    MatchedBy string
    Candidates []EntityCandidate
}
```

## Candidate status

- `exact_match`
- `alias_match`
- `name_match`
- `similarity_match`
- `manual_review_required`

## Query examples

### fingerprint로 entity 찾기

```cypher
MATCH (e:Entity {fingerprint: $fingerprint})
RETURN e
```

### alias 후보 찾기

```cypher
MATCH (e:Entity)
WHERE e.type = $type AND e.aliases_json CONTAINS $alias
RETURN e
```

실제 구현에서는 JSON 검색이 비효율적이면 Alias 노드나 Alias table을 별도로 둘 수 있다.

## Alias를 별도 노드로 둘지

### 단순 MVP

`aliases_json` property를 사용한다.

장점:

- schema 단순
- 구현 빠름

단점:

- alias lookup index 어려움
- provenance 약함

### 확장 모델

`Alias` 노드를 둔다.

```text
(:Alias {value, normalized_value, source})-[:ALIAS_OF]->(:Entity)
```

장점:

- alias provenance
- alias conflict 관리
- index 가능

단점:

- schema 복잡

권장: MVP는 `aliases_json`, v0.4 이후 Alias 노드 검토.

## Policy integration

Ontology file이 dedup rule을 정의한다.

```yaml
entity_types:
  - Project
  - Repository
  - File

dedup:
  Project:
    namespace: organization
    keys: [canonical_name]
  File:
    namespace: repository
    keys: [path]
```

## 결론

Yeoul의 entity resolution은 aggressive auto-merge가 아니라 conservative upsert와 candidate review를 중심으로 한다. 잘못된 병합은 memory 전체의 신뢰를 훼손하므로, Core는 확실한 key 기반 병합만 자동화하고 나머지는 후보로 남긴다.
