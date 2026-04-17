# Current Scope

This document defines the **current working scope** of Yeoul so the repository does not drift into a larger platform before the local implementation toolkit is proven.

## Product definition

Yeoul is a local-first implementation toolkit for temporal agent memory inspired by the Zep and Graphiti architecture.

It uses Ladybug as the embedded graph store.
It exposes developer-facing tools and interfaces for storing, retrieving, and inspecting temporal memory.
It keeps AI extraction behavior and memory-use behavior outside the engine through instructions, skills, ontology files, episode rules, and search recipes.

## Repository intent

At the current stage, this repository should primarily contain:

- documentation for the temporal memory model
- guidance for agent instructions and skills
- storage and retrieval tool specifications
- implementation-facing design docs for a Go + Ladybug local system

It should not expand prematurely into a large runtime platform.

## In scope now

### 1. Paper-aligned memory model

The repository should implement and document the key ideas needed to reproduce the useful parts of the Zep/Graphiti temporal memory architecture:

- Episodes as durable input substrate
- Entities and Facts as graph-level memory objects
- temporal validity and lifecycle tracking
- provenance from derived facts back to source episodes
- graph-aware retrieval primitives

### 2. Local embedded developer tools

The primary implementation target is:

- embedded Go library
- local CLI
- Ladybug-backed schema and transaction model
- replay, inspect, query, and benchmark tooling

### 3. Externalized AI behavior

AI behavior must remain outside the engine.
This repository should therefore include:

- `SKILL.md` guidance
- `agent_instructions.md`
- `ontology.yaml`
- `episode_rules.yaml`
- `search_recipes.yaml`
- examples of policy-aware ingest and retrieval behavior

## Explicitly deferred

The following areas may remain documented, but they are not the primary delivery target right now:

- multi-process daemon mode
- HTTP or gRPC service adapters
- broad plugin ecosystems
- hosted or remote deployment models
- end-user product surfaces
- tightly integrated in-core LLM pipelines

These should be treated as optional future adapters, not active scope drivers.

## Document status guidance

### Active implementation documents

These directly drive the current build:

- overview and principles docs
- Ladybug evaluation and storage constraints
- architecture boundaries
- memory model docs
- Go API
- CLI spec
- policy and skill specs
- implementation docs
- examples

### Reference-only or deferred documents

These remain useful, but should not expand core scope on their own:

- service API
- daemon-specific operational guidance
- plugin or extension discussions beyond policy loading and adapters

## Design rule

When a documentation or implementation choice is unclear, prefer the narrower interpretation:

1. embedded over distributed
2. tool over framework
3. declarative policy over executable AI runtime
4. public memory API over raw graph query exposure
5. inspectable local behavior over platform ambition
