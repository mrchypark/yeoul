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

Current releases support one process owning one database. There is no supported multi-process access mode: the `yeould` service adapter that earlier plans reserved for HTTP or gRPC access is not implemented, and running `yeould` reports that it is unavailable instead of starting a daemon.

Processes that need to share a database must serialize access themselves. Stop the other Yeoul processes before opening the database, and treat a lock failure as "another owner is active" rather than retrying or deleting lock state.

## Why this is explicit

LatticeDB's embedded database is process-owned. Yeoul treats single-process ownership as a product invariant rather than an implementation detail.
