# Provenance Model

상태: Draft v0.1

## 목적

Provenance는 Yeoul memory의 신뢰성과 감사 가능성을 결정한다. Yeoul은 단순히 “기억 결과”를 저장하는 것이 아니라, 각 fact가 어떤 source와 episode에서 왔는지 추적해야 한다.

## 기본 원칙

1. 모든 Fact는 하나 이상의 근거를 가져야 한다.
2. 근거 없는 Fact는 저장할 수 있지만 `confidence`와 `status`에서 낮은 신뢰로 표시해야 한다.
3. Fact가 supersede/retract/contradict 되어도 provenance는 삭제하지 않는다.
4. Search result는 Fact뿐 아니라 supporting Episode/Source를 포함할 수 있어야 한다.
5. Provenance는 human inspection이 가능해야 한다.

## 핵심 객체

### Source

Source는 episode의 원천이다.

예:

- chat thread
- local markdown file
- GitHub issue
- PR comment
- email message
- meeting transcript
- tool result

필드:

- `id`
- `kind`
- `uri`
- `external_ref`
- `created_at`
- `metadata_json`

### Episode

Episode는 Source에서 관찰된 입력 단위다.

필드:

- `id`
- `source_id`
- `kind`
- `content`
- `content_hash`
- `observed_at`
- `ingested_at`
- `metadata_json`

### Fact

Fact는 episode에서 파생된 claim이다.

필드:

- `id`
- `predicate`
- `value_text`
- `confidence`
- `status`
- `valid_from`
- `valid_to`
- `observed_at`
- `created_at`
- `updated_at`

## Provenance relationship

### `(:Episode)-[:ASSERTS]->(:Fact)`

Episode가 Fact를 주장하거나 생성했다는 의미다.

사용 시점:

- 하나의 episode에서 fact가 직접 추출됨
- 수동으로 episode 기반 fact를 등록함

### `(:Fact)-[:DERIVED_FROM]->(:Episode)`

Fact의 근거 episode를 가리킨다. `ASSERTS`와 비슷하지만, `DERIVED_FROM`은 파생 lineage를 명시한다.

권장:

- MVP에서는 `ASSERTS`와 `DERIVED_FROM` 중 하나만 써도 된다.
- 장기적으로는 `ASSERTS`는 episode→fact 생성 관계, `DERIVED_FROM`은 fact→episode 역추적 관계로 모두 유지할 수 있다.

### `(:Episode)-[:MENTIONS]->(:Entity)`

Episode가 Entity를 언급한다.

### `(:Fact)-[:SUBJECT]->(:Entity)`

Fact의 주어 entity.

### `(:Fact)-[:OBJECT]->(:Entity)`

Fact의 목적어 entity. Literal 값은 `value_text`에 저장하고, entity 목적어가 있을 때만 Object edge를 둔다.

### `(:Fact)-[:SUPERSEDES]->(:Fact)`

새 Fact가 이전 Fact를 대체한다.

### `(:Fact)-[:CONTRADICTS]->(:Fact)`

두 Fact가 동시에 참일 수 없거나 충돌 가능성이 있다.

## Provenance strength

검색 ranking에서 provenance의 강도를 사용할 수 있다.

예시 score 구성:

```text
provenance_strength =
  source_weight * source_reliability
  + episode_weight * episode_quality
  + corroboration_weight * supporting_episode_count
```

MVP에서는 단순화한다.

- source exists: +0.3
- episode exists: +0.3
- multiple supporting episodes: +0.2
- source kind trusted: +0.2

## Source reliability

Policy file에서 source reliability를 정의할 수 있다.

```yaml
source_reliability:
  adr: 1.0
  issue_comment: 0.7
  chat_message: 0.5
  tool_result: 0.8
  user_instruction: 0.9
```

Core는 source reliability 의미를 알 필요가 없다. Ranking layer가 policy를 참고한다.

## Provenance query examples

### Fact의 근거 episode 찾기

```cypher
MATCH (f:Fact {id: $fact_id})<-[:ASSERTS]-(e:Episode)-[:FROM_SOURCE]->(s:Source)
RETURN f, e, s
```

### 특정 Source에서 파생된 Fact 찾기

```cypher
MATCH (s:Source {id: $source_id})<-[:FROM_SOURCE]-(e:Episode)-[:ASSERTS]->(f:Fact)
RETURN f, e
```

### Entity에 대한 근거 있는 active fact 찾기

```cypher
MATCH (ent:Entity {id: $entity_id})<-[:SUBJECT]-(f:Fact {status: 'active'})<-[:ASSERTS]-(e:Episode)
RETURN f, e
ORDER BY f.observed_at DESC
```

## Retraction과 provenance

Fact가 retracted 되어도 근거 episode는 남긴다. Retraction은 다음 정보를 포함한다.

- `status = 'retracted'`
- `retraction_reason`
- `retracted_at`
- optional `retracted_by`

Retraction이 발생했다고 해서 source episode를 삭제하면 안 된다. 삭제는 retention policy에 따라 별도 처리한다.

## Privacy consideration

Provenance는 민감한 원문을 포함할 수 있다. 따라서 다음 옵션을 제공해야 한다.

- Episode content redaction
- Source URI masking
- Metadata allowlist
- Retention rule
- Export exclusion

## 결론

Yeoul에서 provenance는 부가 정보가 아니라 core invariant다. Fact가 검색될 때 “무엇이 사실인가”와 함께 “어디서 왔는가”를 대답할 수 있어야 한다.
