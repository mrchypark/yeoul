# Repository Layout

This document defines the intended top-level repository structure for Yeoul.

## Goals
- keep core and adapters separate
- keep policy artifacts inspectable
- keep examples and docs close but distinct
- make contributions easy to route

## Proposed layout

```text
yeoul/
  cmd/
    yeoul/
    yeould/
  internal/
    storage/
      ladybug/
    core/
    query/
    policy/
    lifecycle/
    provenance/
    admin/
  pkg/
    yeoul/
  policies/
    default/
    examples/
  docs/
  examples/
  testdata/
  scripts/
  bench/
```

## Directory responsibilities

### `cmd/yeoul`
CLI entrypoint.

### `cmd/yeould`
Optional local daemon entrypoint.

### `internal/storage/ladybug`
Owns database initialization, migrations, queries, and transaction helpers.

### `internal/core`
Owns domain models and core memory operations.

### `internal/query`
Owns retrieval planning and result assembly.

### `internal/policy`
Owns policy loading and validation.

### `internal/lifecycle`
Owns supersession, retraction, and lifecycle transition logic.

### `internal/provenance`
Owns provenance graph helpers and explanation assembly.

### `internal/admin`
Owns compaction, export, integrity checks, and maintenance helpers.

### `pkg/yeoul`
Public Go API exported for applications.

### `policies`
Reference policy packs shipped with the repository.

### `examples`
Minimal application integrations.

### `testdata`
Golden files, fixtures, and static sample inputs.

### `bench`
Benchmark harnesses and workload generators.

## Code ownership guidance
- storage code should not import agent-pack content
- policy code should not depend on specific adapters
- public API should stay narrow
- examples must use the public API, not internal packages
