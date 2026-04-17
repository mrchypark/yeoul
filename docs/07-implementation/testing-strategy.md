# Testing Strategy

This document defines the implementation-focused testing strategy for Yeoul.

## Goals
- prove correctness of memory invariants
- keep tests runnable locally
- separate fast deterministic tests from heavier integration tests
- catch lifecycle and provenance regressions early

## Test layers

### 1. Unit tests
Scope:
- ID generation helpers
- policy parsing and validation
- lifecycle transition logic
- request normalization
- score and ranking helpers

Requirements:
- no live database
- deterministic
- fast

### 2. Storage integration tests
Scope:
- open/close database
- schema migrations
- create/query node and relationship tables
- transaction rollback behavior
- persistence across restart

Requirements:
- use real Ladybug
- run on CI where supported
- use isolated temp database paths

### 3. Core behavior tests
Scope:
- ingest episode
- derive fact
- provenance traversal
- supersede fact
- retract fact
- entity resolution decisions

Requirements:
- verify graph invariants, not only row counts

### 4. Query API tests
Scope:
- text search request
- timeline request
- neighborhood request
- contradiction checks
- result shaping

### 5. Policy integration tests
Scope:
- load policy pack
- validate schema
- execute recipe into query plan
- prove policy changes alter behavior without changing core code

### 6. CLI tests
Scope:
- command parsing
- JSON output
- exit codes
- destructive command confirmations

### 7. Benchmark regression tests
Scope:
- protect against large performance regressions
- run on representative synthetic workloads

## Golden test strategy
Use golden JSON fixtures for:
- query results
- policy validation errors
- explanation output
- CLI structured output

## Invariant checks
Tests should verify:
- every active fact has provenance
- superseded facts remain queryable historically
- no destructive overwrite happens during lifecycle transitions
- unsupported multi-process assumptions are documented, not silently ignored
