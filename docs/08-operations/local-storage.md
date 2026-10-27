# Local Storage

This document defines local storage ownership for embedded use and the optional `yeould` HTTP daemon.

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
- daemon mode: the operator supplies an explicit database path with `yeould --db`

Released Yeoul v0.10.x and the existing user-level database remain LatticeDB.
This source branch uses embedded local Rhiza for newly created databases on
Linux and macOS; other platforms fall back to Lattice. Opening an existing
LatticeDB path keeps using LatticeDB. Existing Rhiza paths on unsupported
platforms fail closed. Do not point `--create` at an existing database or
change the current user-level path implicitly. Use the explicit migration
procedure in [Storage Migration](storage-migration.md) and verify the new target
before switching an application to it.

## Storage ownership
In embedded mode, the host process owns the database. In daemon mode, one
`yeould` process owns the database and clients use its HTTP API; do not open the
same writable database from other processes. The daemon creates a missing
database only when `--create` is supplied. It defaults to the Rhiza driver in
current releases.

## Local HTTP daemon

```sh
yeould --db "$HOME/.local/share/yeoul/work-memory-rhiza.ltdb" --create
```

This example creates a separate, new database. For an existing LatticeDB user
database, migrate to a new target first; do not run this command against that
existing path. The installed user-level database is not migrated by this source
change.

The `.ltdb` extension remains standard for both storage backends; the directory
contents determine the backend, not the suffix.

The default listener is `127.0.0.1:8080`. Omit `--create` to require an already
existing database. Set `YEOUL_HTTP_TOKEN` to require bearer authentication;
non-loopback listeners are refused without it. The token is read from the
environment, not a command-line argument. The daemon has no built-in TLS; use a
trusted TLS boundary before exposing it beyond a local machine. There is no
physical read-only mode, generic idempotency-key replay, raw backend endpoint,
or checkpoint/admin route. See [Service API](../05-api/service-api.md) for the
route set and request limits.

## Required storage artifacts
- LatticeDB or Rhiza database files/directories, as selected by the database's
  on-disk signature and Yeoul marker
- Yeoul migration metadata
- optional policy cache
- logs and benchmark output outside the database path

Rhiza is opened in local-only mode and is not a supported clustered or
multi-version shared deployment. Rhiza permits only one Yeoul process owner,
including read-only opens. Yeoul's `ReadOnly` setting rejects application
writes, but Rhiza may still perform native recovery writes while opening a
database; it is not a physical read-only guarantee. Ordinary Rhiza mutations
are one atomic delta capped at 96 KiB encoded, including graph/search
projections and metadata. A larger mutation is rejected without splitting or
partial application; episode payloads can hit the cap earlier because their
search projection duplicates text.
Rhiza maintains the public node-local `YeoulRecord.key` property index used by
record upserts and exact-key recovery reads. One public graph-stream event
containing the changed keys is committed atomically with each record delta;
recovery replays that catalog in 64-event pages and fetches records through
parameterized indexed key lookups. The Yeoul catalog is canonical recovery
metadata and must not be trimmed. Startup replay is linear in write history;
introducing a verified snapshot is deferred until measurements justify it.
A full-text query that exceeds Rhiza's result limit returns an error instead
of silently truncating candidates.

The pinned Rhiza qlog package uses Unix-only `syscall.Flock`. Yeoul
isolates the native dependency and disables Rhiza storage on Windows until that
is portable. The separate migration restriction also fails closed before
writes on Windows because directory-entry durability is unavailable through
the current Go implementation.

## Local filesystem guidance
- database path should be writable by the owning process
- avoid shared write access from multiple processes
- temporary benchmark and replay outputs should use separate working directories

## Recommended config fields
- `db_path`
- `mode` (`embedded` or optional `daemon`)
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

### Remote Rhiza backup

The embedded Go API can opt a Rhiza database into remote backup with
`Config.RhizaBackup`. Supported providers are `s3`, `gcs`, and `azure`; configure
the provider's bucket, a nonblank `Prefix`, and credentials using
`RhizaBackupConfig`. Assign a unique prefix to each database and keep it
unchanged when restoring. This setting
does not affect LatticeDB, in-memory databases, the CLI, or the HTTP daemon.
Without it, Yeoul does not configure remote backup.
For S3, `Endpoint` must be `host:port` without a URL scheme; `Insecure` selects
HTTP instead of HTTPS. Azure authentication uses either a storage account key
or a connection string, not both.

The published Yeoul v0.12.0 uses Rhiza v0.20.0; this development version uses
Rhiza v0.21.0 and its fixed local archive format. There is no archive
conversion tool or automatic migration/adoption. For an existing deployment,
keep Yeoul v0.12.0 with its current database and archive, or use this
development version with a fresh database and fresh archive location. The
[Rhiza v0.21.0 release contract](https://github.com/mrchypark/rhiza/releases/tag/v0.21.0)
states that unsupported or partial archives are rejected without modification.
Yeoul has no separate archive-format validator; compatibility with other
existing archives is not guaranteed. Do not use an existing
database/archive location as an upgrade procedure. Existing `.ltdb` extensions
and database paths do not need renaming.

Rhiza local backup uses `BeforeAck`: a successful write acknowledgement means
the corresponding archive durability work completed before the acknowledgement
was returned. This is not a claim of provider parity or a guarantee against
provider-side loss, misconfiguration, or credential revocation. Validate the
selected provider and recovery process in the deployment environment.

Exactly one Yeoul process may own the database and remote backup namespace,
including while restoring. Do not run concurrent writers or restore against a
namespace another writer is using. Rhiza's default node and cluster identity
are not a Yeoul database identity: reusing a bucket/prefix can make unrelated
databases share recovery material. Yeoul does not identify a valid archive
that belongs to a different database; selecting the correct unique prefix is
the operator's responsibility.

`RestoreIfMissing` is a separate, explicit recovery opt-in. It may create a
missing local Rhiza directory so Rhiza can recover it from the configured
remote archive. It cannot be combined with `CreateIfMissing`. Startup fails if
the restored Rhiza catalog does not contain Yeoul's canonical state metadata;
an absent or empty archive must not turn into a silently initialized empty
database. For first-time creation, use `CreateIfMissing` with
`RhizaBackup`, then use `RestoreIfMissing` for a later missing-local-data
recovery. Never use `CreateIfMissing` to recover an existing remote-backed
database.

Yeoul writes a sibling `.yeoul-rhiza-restore-pending` marker before opening a
database for `RestoreIfMissing`. It removes that marker only after the restored
catalog, canonical Yeoul state metadata, and state validation all succeed. If
restore fails or the process stops before validation completes, ordinary opens
(including `CreateIfMissing` and Lattice driver selection) fail closed. Preserve
the marker and local artifacts; verify the bucket, unique prefix, credentials,
and provider availability, then retry with the same `RhizaBackup` and
`RestoreIfMissing` settings. Do not delete the marker or local data to force a
fresh database.

For Rhiza, quiesce the owner and copy its complete native directory and Yeoul
storage marker. A migration target with a pending migration marker is
incomplete and must not be opened or used.

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
