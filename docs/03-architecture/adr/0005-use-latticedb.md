# ADR 0005: Use LatticeDB as the storage engine

## Status

Accepted

## Context

Yeoul needs an embedded property graph that can preserve temporal records,
provenance relationships, and transactional updates without making a native
database runtime the default operational dependency.

## Decision

Use `github.com/mrchypark/latticedb-go` as the default and canonical durable
storage engine. Keep the Ladybug adapter only as a legacy migration reader.

Existing database paths are migrated in place through a verified staging
database. The original Ladybug database remains as a timestamped sibling
backup. The path extension is not changed during automatic migration.

A writable open converts a legacy database automatically. A read-only open is a
no-mutation open: it never converts, and reports a migration requirement
instead, so inspection and backup callers cannot change the source format by
opening it. A read-only caller that does want the conversion opts in explicitly
through `Config.AllowMigration`, or runs `yeoul admin migrate-db`.

## Consequences

- Go 1.27 or newer is required.
- Yeoul records are LatticeDB nodes with indexed IDs and full record payloads.
- Provenance and lifecycle relationships are native graph edges.
- Writes update only changed records and their affected outgoing edges.
- Automatic and explicit migration preserve revision and lifecycle state.
- A read-only open never converts a legacy database without `AllowMigration`.
- Migration requires exclusive ownership of the database path.
- Release builds retain the Ladybug runtime during the migration compatibility window.
- Raw graph queries remain an internal storage concern; public Yeoul APIs are stable.
