# Ladybug Evaluation Plan

## Goal

Determine whether Ladybug is suitable as the embedded storage engine for Yeoul Core.

Ladybug is treated as a schema-first embedded property graph with on-disk and in-memory modes, Cypher support, and a Go API layered over the C API. Evaluation must therefore focus on persistence, migration, concurrency, and recovery behavior, not only basic query success.

## Required Tests

### 1. Basic embedded open

- Open on-disk database
- Create schema
- Insert rows
- Query with Cypher
- Close cleanly

### 2. Persistence

- Insert episodes, entities, and facts
- Close process
- Reopen database
- Verify record counts and indexes

### 3. In-memory mode

- Open `:memory:`
- Insert and query
- Confirm data is lost after process exit

### 4. Single-process concurrency

- Open one READ_WRITE database
- Create N connections
- Run concurrent read and write queries
- Verify no corruption or unexpected errors

### 5. Multi-process lock behavior

- Process A opens READ_WRITE
- Process B attempts READ_WRITE
- Process C attempts READ_ONLY
- Record expected lock behavior

### 6. Query workload

- Entity lookup
- Recent facts
- Episode provenance
- Neighborhood expansion
- Full-text search if enabled
- Vector search if enabled

## Pass Criteria

- Stable on-disk persistence
- Predictable lock behavior
- Safe concurrent use within one process
- No unexplained query failures under expected workload
- Go API is usable without unacceptable cgo friction

## Notes

The documented safe default for Yeoul is a single READ_WRITE Ladybug database object shared through multiple connections within one process. Any deviation from that model must be tested explicitly before it is adopted.
