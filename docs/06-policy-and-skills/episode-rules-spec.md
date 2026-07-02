version: 1

# Episodes preserve source context, evidence, background, and provenance.
# Facts are promoted durable claims backed by supporting episodes.

fact_promotion:
  promote_only:
    - confirmed durable claims
  candidates:
    - decisions
    - durable rules or constraints
    - current status
    - owners
    - corrections or retractions
    - repeated problems and resolutions
    - dependencies or relationships
    - stable preferences
    - definitions or terminology
    - validated evaluation or benchmark conclusions
  require_supporting_episode: true
  clarification_required_when_missing:
    - subject
    - claim
    - scope
    - time_or_status
    - supporting_context
  keep_episode_only:
    - raw progress updates without durable state
    - raw benchmark results without decision impact
    - implementation logs without durable rule or conclusion
    - review notes without reusable decision, correction, rule, relationship, preference, or definition
    - exploratory or ambiguous context

promote_to_episode:
  - name: decision
    when:
      contains_any: ["decided", "agreed", "결정", "합의"]
    priority: high

  - name: durable_rule
    when:
      contains_any: ["rule", "constraint", "always", "never", "원칙", "규칙", "제약"]
    priority: high

  - name: task_assignment
    when:
      contains_any: ["assigned", "owner", "담당", "맡기로"]
    priority: high

  - name: status_change
    when:
      contains_any: ["changed", "resolved", "blocked", "변경", "해결", "막힘"]
    priority: medium

  - name: correction
    when:
      contains_any: ["corrected", "correction", "retracted", "not X but Y", "정정", "철회", "아니라"]
    priority: high

  - name: dependency_relationship
    when:
      contains_any: ["depends on", "dependency", "relationship", "uses", "requires", "의존", "관계", "사용"]
    priority: medium

  - name: preference_or_definition
    when:
      contains_any: ["prefer", "preference", "means", "defined as", "선호", "뜻", "의미", "정의"]
    priority: medium

  - name: validated_conclusion
    when:
      contains_any: ["validated", "benchmark conclusion", "evaluation result", "검증", "벤치", "평가 결과"]
    priority: medium

drop:
  - name: low_signal_ack
    when:
      contains_any: ["ok", "thanks", "확인", "감사", "ㅇㅋ"]
