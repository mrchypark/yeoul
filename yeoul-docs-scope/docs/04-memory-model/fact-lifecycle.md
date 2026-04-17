# Fact Lifecycle

상태: Draft v0.1

## 목적

Yeoul은 시간이 지나며 변하는 사실을 저장한다. Fact lifecycle은 fact가 생성되고, 활성화되고, 대체되고, 충돌하고, 철회되는 과정을 정의한다.

## Fact란 무엇인가

Fact는 episode에서 파생된 claim이다.

예:

- “Yeoul은 Ladybug를 storage engine으로 사용한다.”
- “Core는 AI agent logic을 포함하지 않는다.”
- “Project A의 owner는 Kim이다.”
- “Issue #42는 storage migration 문제와 관련된다.”

Yeoul에서는 Fact를 edge property가 아니라 1급 노드로 둔다. 이유는 다음이다.

- 시간 필드를 붙이기 쉽다.
- provenance를 붙이기 쉽다.
- contradiction/retraction/supersession을 표현하기 쉽다.
- 검색 결과에서 fact 자체를 반환하기 쉽다.

## Fact status

### active

현재 유효한 fact다.

### superseded

새 fact에 의해 대체되었다. 반드시 `SUPERSEDED_BY` 또는 `SUPERSEDES` 관계를 가진다.

### contradicted

다른 fact와 충돌한다. 어느 쪽이 맞는지 확정되지 않을 수 있다.

### retracted

잘못된 정보로 판정되어 철회되었다.

### uncertain

근거가 약하거나 해석이 불확실하다.

### archived

더 이상 active retrieval에서 우선하지 않지만, historical query에서는 남긴다.

## 주요 시간 필드

### observed_at

Source world에서 이 정보가 관찰된 시간.

### ingested_at

Yeoul에 들어온 시간.

### valid_from

Fact가 참이 되기 시작한 시간.

### valid_to

Fact가 참이 아니게 된 시간.

### updated_at

Fact metadata/status가 마지막으로 변경된 시간.

## Lifecycle transition

```text
uncertain -> active
active -> superseded
active -> contradicted
active -> retracted
active -> archived
contradicted -> active
contradicted -> retracted
superseded -> archived
```

## Creation

Fact 생성 시 필요한 최소 입력:

- subject entity
- predicate
- object entity 또는 value text
- source episode
- observed_at 또는 ingested_at

선택 입력:

- confidence
- valid_from
- valid_to
- metadata

## Supersession

새 fact가 이전 fact를 대체할 때 사용한다.

예:

- 이전: “Core implementation language is Rust”
- 신규: “Core implementation language is Go”

처리:

1. 새 fact 생성
2. 기존 fact status를 `superseded`로 변경
3. 기존 fact `valid_to` 설정
4. 새 fact와 기존 fact를 `SUPERSEDES`로 연결

## Contradiction

두 fact가 동시에 참일 수 없지만 어떤 것이 맞는지 확정되지 않았을 때 사용한다.

예:

- Source A: “Project owner is Kim”
- Source B: “Project owner is Lee”

처리:

1. 두 fact 모두 유지
2. 둘 중 하나 또는 둘 모두 status를 `contradicted`로 표시
3. `CONTRADICTS` 관계 생성
4. retrieval에서 contradiction warning 제공

## Retraction

Fact가 잘못되었음이 확인되었을 때 사용한다.

필드:

- `status = retracted`
- `retracted_at`
- `retraction_reason`
- `retracted_by`

Retracted fact는 기본 search에서 제외하되, audit query에서는 조회 가능해야 한다.

## Expiration

유효 기간이 지난 fact를 자동으로 inactive 처리할 수 있다.

예:

- 임시 task assignment
- time-limited preference
- incident status

Expiration은 retraction과 다르다. Fact가 틀린 것이 아니라 더 이상 유효하지 않은 것이다.

## Fact identity

Fact ID는 deterministic 또는 random일 수 있다.

### Random ID

장점:

- 충돌 위험 낮음
- 여러 동일 fact를 별도 provenance로 저장 가능

단점:

- 중복 fact 탐지가 별도 필요

### Deterministic ID

예:

```text
hash(namespace + subject_id + predicate + object_id/value + valid_from)
```

장점:

- 중복 방지 쉬움

단점:

- provenance가 다른 같은 claim을 합칠지 판단이 필요

권장: MVP는 random ID + duplicate candidate detection.

## Retrieval behavior

기본 검색:

- active 우선
- uncertain은 낮은 점수
- superseded/retracted는 제외
- contradicted는 warning과 함께 반환 가능

Historical 검색:

- valid_from/valid_to 기준으로 fact 반환
- superseded fact도 포함 가능

Audit 검색:

- 모든 status 포함
- provenance와 transition history 포함

## 결론

Yeoul의 fact lifecycle은 overwrite를 금지하고 history를 보존한다. Memory engine은 “최신 답”만 만드는 것이 아니라, 시간이 지나며 사실이 어떻게 바뀌었는지 추적할 수 있어야 한다.
