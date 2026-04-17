version: 1

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
