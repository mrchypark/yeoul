# Policy File Specification

Yeoul policy files describe how external systems should use Yeoul Core.

Policy files are not part of the core memory model.
They are declarative configuration for ingest, ontology, retrieval, and agent behavior.

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

`episode_rules.yaml` may declare:
- `fact_promotion`: durable claim classes that can become facts, required clarification fields, and episode-only exclusions.
- `promote_to_episode`: incoming event patterns that should be preserved as source episodes.
- `drop`: low-signal event patterns that should be skipped.

`fact_promotion` is optional for backward compatibility. When present, `require_supporting_episode` must be `true` because facts are promoted claims backed by episodes.
