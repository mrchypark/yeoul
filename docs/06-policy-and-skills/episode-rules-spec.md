# Episode Rules Specification

Episode rules are a single source of truth maintained in the agent pack:

- Canonical file: [`agent-pack/episode_rules.yaml`](../../agent-pack/episode_rules.yaml)

They define fact-promotion candidates, `promote_to_episode` patterns, and
drop rules for episode ingest. The `docs/` tree intentionally does not carry
a copy; keep episode rule changes in the agent pack and reference the file
above.

## `when` matching

`when` clauses accept two token lists:

- `contains_any`: whole-message tokens. A token matches when the episode
  content, after lowercasing and trimming surrounding whitespace and
  punctuation, equals the token. Acknowledgement tokens such as `ok` therefore
  match `OK` and `ok.` but not `broken` or `book`.
- `contains_substring`: explicit substring tokens for rules that intentionally
  look for a phrase inside a longer message.

Whole-message matching is the default so that dropping a low-signal episode
never discards a message that embeds an acknowledgement token alongside a
durable decision.
