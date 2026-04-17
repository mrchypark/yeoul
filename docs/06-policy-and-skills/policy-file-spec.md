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
