version: 1

promote_to_episode:
  - name: decision
    when:
      contains_any: ["decided", "agreed", "결정", "합의"]
    priority: high

  - name: working_preference
    when:
      contains_any: ["prefer", "avoid", "always", "never", "선호", "피하기", "항상", "하지 말"]
    priority: medium

  - name: workflow_guidance
    when:
      contains_any: ["check first", "before starting", "remember to", "먼저 확인", "시작 전에", "참고", "주의"]
    priority: medium

  - name: task_assignment
    when:
      contains_any: ["assigned", "owner", "담당", "맡기로"]
    priority: high

  - name: status_change
    when:
      contains_any: ["changed", "resolved", "blocked", "변경", "해결", "막힘"]
    priority: medium

  - name: recurring_issue
    when:
      contains_any: ["again", "keeps happening", "repeated", "반복", "또 발생", "자주"]
    priority: medium

drop:
  - name: low_signal_ack
    when:
      contains_any: ["ok", "thanks", "확인", "감사", "ㅇㅋ"]
