# Plugin and Extension Model

## Status

Reference architecture only.
For the current product phase, prefer policy packs and thin adapters over a broad plugin system.

This document defines how Yeoul should be extended without turning the core into a framework sprawl.

## Goals
- allow optional integrations
- keep the core small
- avoid runtime dependence on agent frameworks
- make policies and adapters replaceable

## Extension categories

### 1. Storage adapter extensions
Used when Yeoul needs storage-adjacent enhancements.
Examples:
- backup helper
- export/import tools
- schema inspector

These should stay close to the core API and must not bypass core invariants.

### 2. Query and retrieval extensions
Used for custom ranking, recipe execution, or domain-specific post-processing.

These should consume core query results rather than replacing the storage model.

### 3. Policy loader extensions
Used to support additional file formats or validation layers.
Examples:
- YAML loader
- JSON schema validator
- policy version migrator

### 4. Integration adapters
Used to connect Yeoul to other runtimes or local services.
Examples:
- CLI adapter
- HTTP adapter
- gRPC adapter
- MCP adapter
- IDE integration

### 5. Agent Pack extensions
Used to ship skills, instructions, ontologies, and examples for specific domains.

These are not runtime plugins in the core sense; they are packaged policy sets.

## What is not an extension point
- direct injection of LLM clients into the core
- direct prompt execution in the core
- arbitrary code hooks during transaction execution
- replacing lifecycle rules with external side effects

## Recommended design pattern

### Interface-led adapters
The core should expose small interfaces. Adapters sit outside and call the same public API that embedded clients use.

### Declarative-first policy
Before introducing code plugins, prefer configuration and policy files.

### Post-processing over in-transaction mutation
Extension logic should transform inputs before core calls or post-process results after core calls. It should not mutate low-level storage behavior inside core transactions.

## Proposed extension boundaries

### Safe extension boundary
- parse policy files
- build query requests
- rank results
- format output
- expose APIs

### Unsafe extension boundary
- bypassing fact lifecycle invariants
- writing raw Cypher from arbitrary plugin code into the same transaction path
- manipulating underlying storage ownership

## Versioning
Every extension package should declare:
- compatible Yeoul Core version range
- supported policy schema versions
- optional capabilities used

## Recommended package naming
- `yeoul-policy-*`
- `yeoul-adapter-*`
- `yeoul-pack-*`
- `yeoul-export-*`

## Example
A coding-agent pack may ship:
- `SKILL.md`
- `agent_instructions.md`
- `ontology.yaml`
- `search_recipes.yaml`
- a small adapter that maps IDE events to `EpisodeInput`

The adapter lives outside core. The policy pack can be used by other adapters.
