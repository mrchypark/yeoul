# Concurrency Model

Yeoul embedded mode assumes one process owns one read-write LatticeDB database object.

## Allowed

- one process
- one READ_WRITE database object
- multiple connections from that database object
- concurrent queries through those connections

## Not allowed by default

- multiple Yeoul processes writing the same `.ltdb` database
- one writer process plus separate reader processes over the same file
- opening the same database file through multiple independent database objects

## Operational rule

For multi-process use, run `yeould` as a local daemon and access it through HTTP or gRPC.

## Why this is explicit

LatticeDB's embedded database is process-owned. Yeoul treats single-process ownership as a product invariant rather than an implementation detail.
