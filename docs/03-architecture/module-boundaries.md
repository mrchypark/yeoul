# Module Boundaries

## `storage/lattice`

May import LatticeDB.
Owns canonical graph persistence, indexed record lookup, and graph edge projection.
Must not import policy, skills, or agent packages.

## `core`

May depend on storage interfaces.
Defines Episode, Entity, Fact, Source, Query, and Retrieval APIs.
Must not depend on LLMs or agent concepts.

## `policy`

Loads and validates policy files.
May call core APIs.
Must not execute LLM calls.

## `agentpack`

Contains optional instruction templates and skill files.
Must not be imported by core.

## `cmd/yeoul`

CLI for local development, inspection, and benchmarks.

## `cmd/yeould`

Planned local service adapter; not implemented in current releases. The executable reports that no daemon was started and exits with a failure status. Any future implementation must use the same public core API as embedded applications.
