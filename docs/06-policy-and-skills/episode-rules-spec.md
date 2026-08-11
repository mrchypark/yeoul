# Episode Rules Specification

Episode rules are a single source of truth maintained in the agent pack:

- Canonical file: [`agent-pack/episode_rules.yaml`](../../agent-pack/episode_rules.yaml)

They define fact-promotion candidates, `promote_to_episode` patterns, and
drop rules for episode ingest. The `docs/` tree intentionally does not carry
a copy; keep episode rule changes in the agent pack and reference the file
above.
