# Storage Architecture

Yeoul stores all durable memory in a Ladybug on-disk database.

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
