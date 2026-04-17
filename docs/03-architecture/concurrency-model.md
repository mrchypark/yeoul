# Concurrency Model

Yeoul embedded mode assumes one process owns one READ_WRITE Ladybug database object.

## Allowed

- one process
- one READ_WRITE database object
- multiple connections from that database object
- concurrent queries through those connections

## Not allowed by default

- multiple Yeoul processes writing the same `.lbug` database
- one writer process plus separate reader processes over the same file
- opening the same database file through multiple independent database objects

## Operational rule

For multi-process use, run `yeould` as a local daemon and access it through HTTP or gRPC.

## Why this is explicit

Ladybug's documented safe default is connection-level concurrency under a single owning READ_WRITE database object. Yeoul treats that as a product invariant rather than an implementation detail.
