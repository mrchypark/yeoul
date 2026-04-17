# Data Retention

상태: Draft v0.1

## 목적

Yeoul memory는 장기간 누적된다. Data retention은 어떤 데이터를 얼마나 오래 보존하고, 언제 archive/delete/redact할지 정의한다.

## 원칙

1. Retention은 policy-driven이어야 한다.
2. 기본 동작은 삭제가 아니라 보존이다.
3. Destructive deletion은 dry-run과 confirmation을 요구한다.
4. Provenance를 끊는 삭제는 경고해야 한다.
5. Retention action은 audit 가능해야 한다.

## Retention 대상

- Source
- Episode
- Entity
- Fact
- Relationship
- metadata_json
- raw content

## Retention action

### keep

아무 작업도 하지 않는다.

### archive

검색 기본 결과에서 낮은 우선순위로 둔다. Historical/audit query에서는 조회 가능하다.

### redact

민감한 content나 metadata field를 제거한다.

### delete

물리 삭제한다. MVP에서는 제한적으로만 허용한다.

### summarize

여러 episode를 summary episode로 압축한다. Core는 LLM summarization을 하지 않는다. 외부 summarizer가 만든 summary를 저장할 수 있다.

## Policy example

```yaml
retention:
  defaults:
    action: keep
  rules:
    - name: drop_low_signal_chat_after_30d
      target: Episode
      when:
        kind: chat_message
        metadata.priority: low
        older_than_days: 30
      action: archive
    - name: redact_email_body_after_180d
      target: Episode
      when:
        source_kind: email
        older_than_days: 180
      action: redact
      fields: [content]
```

## Dry-run report

```json
{
  "rules_evaluated": 2,
  "episode_archive_candidates": 42,
  "episode_redact_candidates": 7,
  "fact_impacts": [
    {
      "fact_id": "fact_...",
      "issue": "would lose raw episode content but keep source reference"
    }
  ]
}
```

## CLI

```bash
yeoul retention plan --policy ./policies/default --dry-run
yeoul retention apply --policy ./policies/default --confirm
```

## Entity retention

Entity deletion is risky. If an entity is deleted, many facts may lose subject/object structure. MVP는 entity physical deletion을 제공하지 않는다.

대신:

- status = archived
- status = merged
- metadata redaction

## Fact retention

Active fact는 기본적으로 삭제하지 않는다. Superseded/retracted fact도 audit value가 있으므로 장기 보존을 권장한다.

삭제 가능 후보:

- low confidence uncertain fact
- duplicate fact
- policy-expired fact with no unique provenance value

## Source/Episode retention

Episode는 민감 원문을 포함할 수 있어 retention 대상이 된다. 그러나 fact provenance를 유지하기 위해 다음 선택지를 제공한다.

- raw content redact
- content hash preserve
- source metadata preserve
- episode node preserve

## 결론

Retention은 단순 삭제 기능이 아니다. Temporal graph memory에서 retention은 privacy, provenance, historical value 사이의 균형이다. MVP에서는 archive/redact 중심으로 시작하고 physical delete는 제한한다.
