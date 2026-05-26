# Storage Architecture

Yeoul stores all durable memory in a Ladybug on-disk database.

When Yeoul uses an external retrieval runtime, that runtime must remain derived from the Ladybug-backed canonical store rather than becoming an equal source of truth.

See [ladybug-plus-rax.md](./ladybug-plus-rax.md) for the recommended dual-store architecture.

## Default path

`./yeoul.lbug`

## Modes

- on-disk: durable, production default
- in-memory: test and temporary analysis only

## Storage ownership

In embedded mode, the host process owns the database.
In daemon mode, `yeould` owns the database.

## Raw Cypher

Raw Cypher is used internally by the storage adapter.
Application code should use Yeoul APIs.
