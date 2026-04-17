# Module Boundaries

## `storage/ladybug`

May import Ladybug Go bindings.
May execute Cypher.
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

CLI for local development, inspection, migration, and benchmarks.

## `cmd/yeould`

Optional local daemon.
Must use the same public core API as embedded applications.
