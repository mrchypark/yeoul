# Local Storage

This document defines how Yeoul uses local storage in embedded mode. Daemon mode is planned and not available in current releases.

## Goals
- predictable storage location
- simple operator mental model
- safe local ownership
- easy backup and cleanup

## Database path
Yeoul should default to an explicit configured path.
Example defaults:
- embedded app: application-controlled path
- CLI quickstart: `./yeoul.ltdb`
- daemon mode: not available in current releases (planned user-configurable application data directory)

## Storage ownership
In embedded mode, the host process owns the database.
Daemon mode is not available in current releases: the `yeould` service adapter is deferred, so every supported deployment uses embedded ownership.

## Required storage artifacts
- LatticeDB database files/directories
- Yeoul migration metadata
- optional policy cache
- logs and benchmark output outside the database path

## Local filesystem guidance
- database path should be writable by the owning process
- avoid shared write access from multiple processes
- temporary benchmark and replay outputs should use separate working directories

## Recommended config fields
- `db_path`
- `mode` (`embedded`; `daemon` is reserved and unavailable in current releases)
- `checkpoint_policy`
- `log_path`
- `export_path`
- `policy_path`

## Cleanup policy
Yeoul should support:
- deleting temp benchmark databases
- exporting before destructive maintenance
- dry-run reporting for retention and compaction jobs

## Native backup and restore

The supported full-fidelity backup is a quiesced native-directory copy:

1. Stop every Yeoul process that can open the database, so no writer holds the
   database or its ownership lock.
2. Copy the whole database file set to a new path. A LatticeDB database is a
   directory named after the database path (for example `yeoul.ltdb/`) plus any
   sibling namespace entries sharing that name, such as
   `yeoul.ltdb.yeoul-migration.lock`. Copy the directory contents and the
   sibling entries together; do not copy only the top-level file.
3. Exclude process-scoped `*.lock` files, or omit them from the copy. They are
   recreated on the next open, and a stale copied lock is not meaningful.
4. Restore by pointing a new database path at the copied file set, or by
   replacing the original while every process is stopped.

This preserves records, revision history, and historical query results. It is
verified by `pkg/yeoul/native_backup_test.go`, which copies a quiesced database
and confirms matching counts, revisions, and as-of query results after reopen.

`yeoul admin export` is not a temporal backup. It refuses to produce an
importable snapshot when revision history, inactive facts, or lifecycle metadata
are present, and import re-ingests records at new times. Keep the native copy as
the full-fidelity backup and use `admin export` only for logical record transfer.

## Corruption posture
Yeoul should assume the underlying database is authoritative and must be opened only through supported ownership paths.
If corruption or lock conflict is suspected, operator tooling should stop and inspect rather than trying hidden repairs.
