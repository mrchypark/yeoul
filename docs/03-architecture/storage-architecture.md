# Storage Architecture

Yeoul stores all durable memory in a LatticeDB on-disk property graph.

## Default path

`./yeoul.ltdb`

New databases conventionally use LatticeDB's `.ltdb` extension. The extension
does not identify the on-disk format; a migrated LatticeDB directory may retain
an older `.lbug` name.

## Modes

- on-disk: durable, production default
- in-memory: test and temporary analysis only

## Storage ownership

In embedded mode, the host process owns the database.
Daemon mode is not available in current releases: the `yeould` service adapter is deferred, so every supported deployment uses embedded ownership.

## Graph projection

Each source, episode, entity, fact, revision, and migration watermark is stored
as a typed node with an indexed Yeoul ID and a full-fidelity payload. Provenance
and lifecycle relationships are projected as native LatticeDB edges.

Application code uses Yeoul APIs over LatticeDB. Current releases do not read
or migrate native Ladybug databases.
