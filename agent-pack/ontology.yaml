version: 1

entity_types:
  - Person
  - Organization
  - Project
  - Task
  - Document
  - Repository
  - File
  - Decision
  - Issue

predicates:
  - OWNS
  - WORKS_ON
  - DECIDED
  - BLOCKED_BY
  - DEPENDS_ON
  - MENTIONED_IN
  - CHANGED_TO
  - SUPERSEDES

dedup:
  Person:
    keys: [email, canonical_name]
  Repository:
    keys: [url, canonical_name]
  File:
    keys: [path, repository]
