version: 1

episode_role:
  summary: Episodes preserve source context, evidence, background, and provenance.
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
    keep_episode_only:
      - raw progress updates without durable state
      - raw benchmark results without decision impact
      - implementation logs without a durable rule or conclusion
      - review notes without a reusable decision, correction, or rule
      - exploratory or ambiguous context

promote_to_episode:
  - name: decision
    when:
      contains_any: ["decided", "agreed", "결정", "합의"]
    priority: high

  - name: task_assignment
    when:
      contains_any: ["assigned", "owner", "담당", "맡기로"]
    priority: high

  - name: status_change
    when:
      contains_any: ["changed", "resolved", "blocked", "변경", "해결", "막힘"]
    priority: medium

drop:
  - name: low_signal_ack
    when:
      contains_any: ["ok", "thanks", "확인", "감사", "ㅇㅋ"]
