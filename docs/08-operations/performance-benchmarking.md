# Performance Benchmarking

This document defines how performance should be measured for Yeoul.

## Benchmark goals
- validate that Yeoul is practical as a local embedded memory engine
- find query and ingest bottlenecks early
- compare schema and index choices
- protect against regressions

## Benchmark classes

### Ingest benchmarks
Measure:
- episodes per second
- facts per second
- entity resolution overhead
- transaction batch effects

### Query benchmarks
Measure:
- entity lookup latency
- recent-context search latency
- neighborhood expansion latency
- provenance explanation latency
- timeline query latency

### Lifecycle benchmarks
Measure:
- supersede throughput
- retract throughput
- historical query overhead after many lifecycle transitions

### Compaction benchmarks
Measure:
- candidate scan duration
- applied merge throughput
- storage size effect
- post-compaction query behavior

## Dataset classes
- tiny (developer sanity)
- small (10k episodes)
- medium (100k episodes)
- large (1M+ facts, synthetic if needed)

## Reporting
Benchmark reports should include:
- machine profile
- OS
- Go version
- Ladybug version
- Yeoul version or git SHA
- dataset shape
- policy pack used
- p50/p95/p99
- throughput
- peak memory usage

## Benchmark discipline
- warm vs cold runs should be noted
- on-disk and in-memory runs should be distinguished
- results should not be compared across schema versions without annotation
- benchmark scripts should live in version control

## Success criteria for MVP
The benchmark suite should prove:
- Yeoul can ingest and query non-trivial local datasets
- restart and persistence do not break queryability
- normal local workflows stay within acceptable latency bounds
