# Documentation Map

Yeoul documentation is split into two major bundles:

- `docs/`: Yeoul Core, product, architecture, memory model, APIs, and operations.
- `agent-pack/`: AI-agent-facing skills, instructions, ontology, episode rules, and search recipes.

The governing separation rule is:

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```

## Planned Tree

```text
docs/
  00-overview/
    vision.md
    glossary.md
    principles.md
    non-goals.md

  01-product/
    prd.md
    personas.md
    use-cases.md
    roadmap.md
    packaging.md

  02-research/
    graphiti-analysis.md
    ladybug-evaluation-plan.md
    local-embedded-db-requirements.md
    alternatives.md

  03-architecture/
    system-architecture.md
    module-boundaries.md
    storage-architecture.md
    concurrency-model.md
    plugin-extension-model.md
    adr/
      0001-use-ladybug.md
      0002-go-core.md
      0003-agent-free-core.md
      0004-policy-files.md

  04-memory-model/
    conceptual-model.md
    schema.md
    temporal-semantics.md
    provenance.md
    entity-resolution.md
    fact-lifecycle.md
    compaction.md

  05-api/
    go-api.md
    cli-spec.md
    service-api.md
    query-api.md
    error-model.md

  06-policy-and-skills/
    policy-file-spec.md
    skill-file-spec.md
    ontology-spec.md
    episode-rules-spec.md
    search-recipes-spec.md
    agent-instruction-pack.md

  07-implementation/
    repo-layout.md
    migration-system.md
    ladybug-cypher-ddl.md
    indexing.md
    transactions.md
    testing-strategy.md

  08-operations/
    local-storage.md
    backup-restore.md
    observability.md
    performance-benchmarking.md
    security-and-privacy.md
    data-retention.md

  09-quality/
    acceptance-criteria.md
    test-plan.md
    benchmark-plan.md
    failure-modes.md

  10-examples/
    quickstart.md
    example-skill.md
    example-ontology.md
    example-agent-instructions.md
    example-ingest-workflow.md
```

## Current Scope

This repository now includes:

- the MVP core documentation set
- most of the remaining product, research, memory model, API, implementation, operations, quality, and example drafts
- the separate `agent-pack/` starter files for AI agent integrations

Some planned files in the tree are still intentionally pending and can be added as implementation needs become concrete.

## Scope Anchor

Use [current-scope.md](/Users/cypark/Documents/project/yeoul/docs/00-overview/current-scope.md) as the primary reference for what is actively in scope versus documented-but-deferred.

In short:

- active scope: local embedded toolkit, CLI, policy guidance, memory model, and implementation docs
- deferred scope: daemon/service mode and broader extension surfaces
