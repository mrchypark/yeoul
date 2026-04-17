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
