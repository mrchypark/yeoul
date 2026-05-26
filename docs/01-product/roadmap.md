# Roadmap

This roadmap defines staged delivery for Yeoul. Dates are intentionally omitted; stages should be converted into milestones after the first Ladybug validation pass.

For the recommended product shape beyond the MVP core, see [../03-architecture/v0.2-product-architecture.md](../03-architecture/v0.2-product-architecture.md).

## Stage 0: Foundation and validation

### Goals
- validate Ladybug as the embedded storage engine
- validate Go integration and build constraints
- freeze product scope and non-goals
- define schema and public engine interfaces

### Deliverables
- Ladybug evaluation report
- initial schema draft
- engine interface draft
- local persistence smoke tests
- repository scaffolding
- documentation baseline

### Exit criteria
- Yeoul can open a local database, create schema, insert data, and reopen successfully
- concurrency assumptions are documented and tested
- raw Cypher remains internal to storage code

## Stage 1: Minimum viable core

### Goals
- implement embedded Yeoul Core
- support Episode, Entity, Fact, and Source
- support basic retrieval and provenance traversal

### Deliverables
- `Open` / `Close`
- `IngestEpisode`
- `UpsertEntity`
- `AssertFact`
- `Search`
- `Neighborhood`
- schema migrations
- CLI skeleton

### Exit criteria
- database survives process restart
- provenance from Fact to Episode works
- basic recent-context query works

## Stage 2: Temporal and lifecycle correctness

### Goals
- make fact history usable and explicit
- define supersede/retract behavior
- add contradiction checks

### Deliverables
- fact lifecycle APIs
- `SupersedeFact`
- `RetractFact`
- active vs historical retrieval filters
- lifecycle integrity tests

### Exit criteria
- fact changes never require destructive overwrite
- current vs historical state are both queryable

## Stage 3: Policy and skills externalization

### Goals
- make Yeoul usable by agent harnesses without coupling the core to agent code

### Deliverables
- policy loader
- validation for ontology, episode rules, and search recipes
- example skill pack
- example agent instructions

### Exit criteria
- memory behavior can be changed by editing policy files
- core still functions without policy files

## Stage 4: Query quality and ergonomics

### Goals
- improve retrieval quality and developer usability
- establish the retrieval layer as a product surface rather than a thin helper around primitive queries

### Deliverables
- query API expansion
- recipe executor
- richer result views
- ranking improvements
- search projection and rebuild flow
- bounded rerank model
- context constructor outputs
- graph inspection commands
- export/import helpers

### Exit criteria
- common memory queries work without raw Cypher
- output is stable enough for agent-facing adapters
- retrieval quality, latency, and context cost are measurable together

## Stage 5: Agent-facing workflow layer

### Goals
- make common memory workflows directly usable from CLI and adapters without exposing primitive orchestration

### Deliverables
- `yeoul wake-up`
- `yeoul context`
- `yeoul decision prepare`
- `yeoul status explain`
- `yeoul context deep`
- stronger MCP and skill integration contracts

### Exit criteria
- common agent workflows can be expressed with intent-level commands
- wake-up and context loading do not require durable summary storage

## Stage 6: Optional local daemon

This stage is intentionally deferred until the embedded toolkit, CLI, and policy-driven local workflow are stable.

### Goals
- support multiple local processes safely

### Deliverables
- `yeould`
- HTTP/gRPC API
- local auth or socket binding strategy
- daemon observability and admin commands

### Exit criteria
- multiple local clients can access the same Yeoul database through a single owner process

## Stage 7: Hardening and release

### Goals
- benchmark, document, package, and stabilize

### Deliverables
- benchmark suite
- retrieval quality suite
- temporal correctness suite
- retention/redaction controls
- backup/restore tooling
- release packaging
- contributor docs

### Exit criteria
- performance envelope documented
- test coverage meets release threshold
- operator docs complete

## Deferred roadmap items
- wasm or browser-facing adapter
- remote hosted mode
- replication or distributed operation
- non-Ladybug backend support
