# Compaction

상태: Draft v0.1

## 목적

Yeoul은 episode와 fact를 계속 누적한다. 시간이 지나면 중복 episode, 오래된 fact, 낮은 가치의 source, 병합 가능한 entity가 늘어난다. Compaction은 memory의 크기를 줄이고 검색 품질을 유지하기 위한 정리 과정이다.

## 원칙

1. Compaction은 기본적으로 destructive하지 않아야 한다.
2. 모든 destructive compaction은 `--dry-run`을 먼저 제공해야 한다.
3. Fact provenance를 끊으면 안 된다.
4. Active fact를 삭제하지 않는다.
5. Compaction 결과는 audit log에 남긴다.

## Compaction 종류

### 1. Episode compaction

중복되거나 낮은 가치의 episode를 정리한다.

후보:

- 같은 content_hash를 가진 episode
- source 재수집으로 중복 생성된 episode
- ack/low-signal message
- retention 기간이 지난 episode

동작:

- 삭제 전 fact provenance 영향 분석
- 삭제 대신 archived status 부여 가능
- content redaction만 수행 가능

### 2. Fact compaction

중복 fact와 오래된 superseded fact를 정리한다.

후보:

- 동일 subject/predicate/object의 active duplicate
- 오래된 superseded fact
- retracted fact 중 retention 기간이 지난 것

동작:

- duplicate fact 병합
- supporting episodes는 보존
- representative fact 선택

### 3. Entity compaction

중복 entity를 병합 후보로 제안한다.

후보:

- 같은 fingerprint
- 같은 external ID
- 같은 namespace + canonical name
- alias overlap

동작:

- 자동 병합은 exact key에 한정
- fuzzy candidate는 review list 생성
- merge relation을 남김

### 4. Metadata compaction

큰 metadata_json을 정리하거나 allowlist 기반으로 축소한다.

후보:

- oversized metadata
- unused fields
- sensitive field

동작:

- redaction
- field pruning
- external archive reference로 대체

## Compaction workflow

```text
scan -> candidate generation -> impact analysis -> dry-run report -> apply -> audit log
```

### Scan

Compaction 대상 범위를 결정한다.

범위:

- entire database
- namespace
- source
- project
- time range
- entity type

### Candidate generation

rule에 따라 후보를 만든다.

### Impact analysis

삭제/병합이 provenance와 retrieval에 미치는 영향을 계산한다.

예:

- 이 episode를 삭제하면 몇 개 fact가 source를 잃는가?
- 이 entity merge가 몇 개 fact의 subject/object를 바꾸는가?
- 이 fact archive가 active search result에 영향을 주는가?

### Dry-run report

적용 전 보고서를 출력한다.

```json
{
  "scope": "project:yeoul",
  "episode_candidates": 120,
  "fact_candidates": 34,
  "entity_merge_candidates": 7,
  "blocking_issues": [
    "12 facts would lose their only source episode"
  ]
}
```

### Apply

사용자가 승인한 작업만 적용한다.

### Audit log

모든 변경은 audit record로 남긴다.

## CLI 예시

```bash
yeoul compact --scope project:yeoul --dry-run
yeoul compact episodes --older-than 180d --dry-run
yeoul compact facts --status superseded --older-than 365d --apply
yeoul compact entities --type Person --candidates-only
```

## Retention과 차이

Retention은 정책 기반 삭제/보존이다. Compaction은 품질과 효율을 위한 정리다. 두 기능은 겹칠 수 있지만 목적이 다르다.

- Retention: 법적/운영상 보존 기간
- Compaction: 중복/노이즈/검색 품질 관리

## Safety rules

- Active fact의 유일한 source episode를 삭제하지 않는다.
- Entity merge는 rollback metadata를 남긴다.
- Retraction history는 retention rule 없이는 삭제하지 않는다.
- Compaction은 기본적으로 online query보다 낮은 priority로 실행한다.

## Metrics

Compaction 후 다음 지표를 기록한다.

- episode count before/after
- fact count before/after
- entity count before/after
- database file size before/after
- search latency before/after
- candidate false positive rate

## 결론

Compaction은 단순 vacuum이 아니다. Temporal graph memory에서 compaction은 provenance와 lifecycle을 지키면서 memory 품질을 유지하는 작업이다. MVP에서는 dry-run report와 duplicate candidate generation부터 구현한다.
