# Roadmap

This roadmap defines staged delivery for Yeoul. Dates are intentionally omitted; stages should be converted into milestones after the first Ladybug validation pass.

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

### Deliverables
- query API expansion
- recipe executor
- richer result views
- ranking improvements
- graph inspection commands
- export/import helpers

### Exit criteria
- common memory queries work without raw Cypher
- output is stable enough for agent-facing adapters

## Stage 5: Optional local daemon

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

## Stage 6: Hardening and release

### Goals
- benchmark, document, package, and stabilize

### Deliverables
- benchmark suite
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
