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

## Corruption posture
Yeoul should assume the underlying database is authoritative and must be opened only through supported ownership paths.
If corruption or lock conflict is suspected, operator tooling should stop and inspect rather than trying hidden repairs.
