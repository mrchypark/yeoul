# ADR 0004: Externalize behavior through policy files

## Status

Accepted

## Decision

Use Markdown and YAML files for skills, ontology, episode rules, and search recipes.

## Consequences

- Behavior can change without recompiling the core.
- Policies need schema validation.
- Core correctness must not depend on a specific policy pack.
