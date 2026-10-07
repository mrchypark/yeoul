# Policy File Specification

Yeoul policy files describe how external systems should use Yeoul Core.

Policy files are not part of the core memory model. They provide declarative
guidance to integrating agents and callers; they do not make Core perform
automatic extraction or fact promotion.

## File Types

- `SKILL.md`
- `ontology.yaml`
- `episode_rules.yaml`
- `search_recipes.yaml`
- `agent_instructions.md`

## Policy Directory

```text
policies/
  default/
    SKILL.md
    ontology.yaml
    episode_rules.yaml
    search_recipes.yaml
    agent_instructions.md
```

## Rule

Core must run without policy files.
Policy files enhance behavior but do not define storage correctness.

## `episode_rules.yaml`

`episode_rules.yaml` may describe agent guidance for:
- `fact_promotion`: durable claim classes that can become facts, required clarification fields, and episode-only exclusions.
- `promote_to_episode`: incoming event patterns that should be preserved as source episodes.
- `drop`: low-signal event patterns that should be skipped.

The CLI validates these fields, but the rules do not automatically drop,
preserve, or promote ingested content. The agent or caller applies them.
When `fact_promotion` is present, `require_supporting_episode` must be `true`.
