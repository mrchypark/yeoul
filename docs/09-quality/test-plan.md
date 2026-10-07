# Test Plan

This document defines the release-oriented quality plan for Yeoul.

## Objectives
- verify correctness of the temporal memory model
- verify local embedded operability
- verify policy-pack integration
- verify lifecycle and provenance safety
- verify acceptable performance for MVP

## Test matrix

### Functional
- init database
- migrate schema
- ingest episodes
- upsert entities
- assert facts
- search
- neighborhood
- fact explain
- supersede fact
- retract fact
- validate policy pack

### Persistence
- reopen database after clean shutdown
- reopen database after multiple writes
- verify counts and sample queries

### Concurrency
- one process, one write-capable database owner, many connections
- expected lock conflict behavior in unsupported ownership models

### Policy
- valid pack loads
- invalid pack fails clearly
- policy changes alter behavior at the policy layer without code changes

### CLI
- command success cases
- JSON output format
- exit codes
- confirmation paths

### Service
- request/response correctness
- error mapping
- daemon ownership model (planned; unavailable until the daemon ships)

## Test environments
- local developer machine
- CI Linux runner
- optional macOS runner if packaging requires it

## Release gate
The active release workflow checks its configured build and test jobs. A
benchmark regression threshold is planned, but is not currently measured or
enforced by `release.yml`; do not treat it as an active release gate until a
baseline and check are implemented.

## Planned benchmark gate
When a measured threshold is defined and wired into the release workflow, a
release candidate should not be cut unless:
- core functional suite passes
- persistence suite passes
- lifecycle suite passes
- policy validation suite passes
- benchmark regression check passes the documented threshold
