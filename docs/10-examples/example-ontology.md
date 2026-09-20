# Example Ontology

```yaml
version: 1

entity_types:
  - Person
  - Repository
  - Project
  - File
  - Issue
  - PullRequest
  - Decision
  - Service
  - Dependency

predicates:
  - OWNS
  - MAINTAINS
  - WORKS_ON
  - DECIDED
  - DEPENDS_ON
  - BLOCKED_BY
  - FIXED_BY
  - RELATED_TO
  - CHANGED_TO

extensions:
  exclusive_predicates:
    - CURRENT_OWNER

dedup:
  Person:
    keys: [email, canonical_name]
  Repository:
    keys: [url, canonical_name]
  File:
    keys: [repository, path]
  Issue:
    keys: [tracker, external_id]
```

`extensions` carries advisory content Yeoul Core does not interpret. Put
exclusivity hints and other non-core notes there; unknown top-level keys
outside `extensions` fail `yeoul policy validate` so that misspelled
structural fields are caught instead of silently ignored.
