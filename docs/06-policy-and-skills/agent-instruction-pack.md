# Agent Instruction Pack

Yeoul Agent Pack is a repository of policy artifacts that tell external agents how to use Yeoul memory well.

## Purpose
The instruction pack exists so that:
- Yeoul Core stays agent-free
- memory behavior remains inspectable
- multiple agents can share one memory substrate
- policy changes can be reviewed as files

## Pack contents
A standard agent instruction pack should contain:

```text
pack/
  README.md
  SKILL.md
  agent_instructions.md
  ontology.yaml
  episode_rules.yaml
  search_recipes.yaml
  examples/
```

## Responsibilities of each file

### `SKILL.md`
Human-readable summary of when to remember, when to search, and what to ignore.

### `agent_instructions.md`
Longer behavioral instructions tailored to a class of agent or workflow.

### `ontology.yaml`
Allowed or preferred entity types, predicates, dedup keys, and exclusivity rules.

### `episode_rules.yaml`
Rules that determine which incoming events should become episodes.

### `search_recipes.yaml`
Named retrieval patterns.

## Pack principles
- no executable code required
- explicit versioning
- explainability over cleverness
- domain specificity is allowed
- packs may be shared or layered

## Pack layering
A deployment may combine:
- a base Yeoul pack
- a domain pack
- a project-specific override pack

The loader should define precedence explicitly.

## Example pack types
- coding agent pack
- research agent pack
- operations incident pack
- personal assistant pack

## Validation requirements
A valid pack must:
- declare version
- validate each YAML file against expected schema
- contain at least one skill or instruction file
- not require network calls to parse

## Runtime boundary
Instruction packs should never:
- modify storage transactions directly
- bypass lifecycle rules
- assume raw Cypher access in prompts
- depend on a specific LLM provider in the core runtime

## Product packaging
The Agent Pack should be published as optional content, separate from Yeoul Core, even when shipped in the same repository.
