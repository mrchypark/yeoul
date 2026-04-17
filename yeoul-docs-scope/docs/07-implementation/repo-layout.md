# Repository Layout

상태: Draft v0.1

## 목적

Yeoul repository의 패키지 구조와 책임 경계를 정의한다. 핵심은 Core와 Agent Pack을 분리하는 것이다.

## 제안 구조

```text
yeoul/
  go.mod
  README.md
  LICENSE

  cmd/
    yeoul/
      main.go
    yeould/
      main.go

  internal/
    storage/
      ladybug/
        db.go
        conn.go
        tx.go
        migrate.go
        query.go
        errors.go
    migration/
      migration.go
      registry.go
    validate/
      input.go
      policy.go

  pkg/
    yeoul/
      engine.go
      config.go
      errors.go
    model/
      episode.go
      entity.go
      fact.go
      source.go
      provenance.go
      time.go
    ingest/
      pipeline.go
      episode.go
      entity.go
      fact.go
    retrieval/
      search.go
      query.go
      ranking.go
      neighborhood.go
    policy/
      loader.go
      ontology.go
      episode_rules.go
      search_recipes.go
      validation.go

  policies/
    default/
      SKILL.md
      agent_instructions.md
      ontology.yaml
      episode_rules.yaml
      search_recipes.yaml

  docs/
    ...

  examples/
    embedded-basic/
    cli-workflow/
    agent-pack-only/

  testdata/
    episodes/
    policies/
    db/
```

## Package rules

### `pkg/yeoul`

Public entrypoint. Exposes Engine interface and config.

허용:

- model
- ingest
- retrieval
- policy interface

금지:

- raw Ladybug binding 직접 노출
- LLM SDK import
- agent runtime import

### `pkg/model`

Domain model definitions.

포함:

- Episode
- Entity
- Fact
- Source
- Provenance
- IDs
- Time fields

### `pkg/ingest`

Episode/entity/fact 저장 pipeline.

역할:

- input validation
- entity resolution 호출
- fact assertion 호출
- transaction boundary 구성

### `pkg/retrieval`

Search, query, ranking.

역할:

- SearchRequest 해석
- query plan 생성
- candidate search
- ranking
- provenance hydration

### `pkg/policy`

Policy files loader and validator.

포함:

- ontology parser
- episode rule parser
- search recipe parser
- skill metadata parser

주의:

- policy package는 Core를 보조한다.
- policy package는 AI execution을 하지 않는다.

### `internal/storage/ladybug`

Ladybug adapter.

역할:

- DB open/close
- connection management
- Cypher execution
- transaction mapping
- error mapping

주의:

- 이 패키지 밖에서는 Ladybug binding을 직접 import하지 않는다.

### `cmd/yeoul`

CLI.

역할:

- init
- migrate
- ingest
- search
- inspect
- policy validate
- bench

### `cmd/yeould`

Optional local daemon.

역할:

- DB owner process
- HTTP/gRPC API
- multi-process access coordination

## Test organization

```text
testdata/
  policies/
    valid-default/
    invalid-missing-version/
    invalid-unknown-predicate/
  episodes/
    simple-chat.jsonl
    project-decisions.jsonl
  expected/
    search-recent-decisions.json
```

## Build tags

Ladybug Go binding이 cgo를 사용할 가능성이 있으므로 build tags를 고려한다.

```text
//go:build ladybug
//go:build !ladybug
```

Fallback mock storage는 test에 사용한다.

## Dependency rule

```text
cmd -> pkg/yeoul -> pkg/model/pkg/ingest/pkg/retrieval -> internal/storage
policy -> model
agent pack files -> no Go import
```

## 결론

Repository layout은 Yeoul의 철학을 반영해야 한다. Agent Pack은 파일 세트로 존재하고, Core는 Go package로 존재하며, storage adapter는 내부 구현으로 격리한다.
