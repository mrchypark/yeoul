# Performance Benchmarking

상태: Draft v0.1

## 목적

Yeoul의 성능을 측정하는 기준을 정의한다. Benchmark는 최적화보다 먼저 baseline과 regression detection을 위해 필요하다.

## Benchmark 원칙

1. Synthetic workload와 realistic workload를 모두 사용한다.
2. Ingest와 search를 분리 측정한다.
3. p50/p95/p99 latency를 기록한다.
4. DB file size와 memory usage를 함께 기록한다.
5. Ladybug version과 Go version을 기록한다.

## Benchmark dimensions

### Dataset size

- 1k episodes
- 10k episodes
- 100k episodes
- 1M facts

### Graph shape

- sparse: episode당 entity 1~3개, fact 1개
- normal: episode당 entity 3~7개, fact 2~5개
- dense: episode당 entity 10개 이상, fact 10개 이상

### Query type

- entity lookup
- recent facts
- provenance lookup
- keyword search
- neighborhood 1-hop
- neighborhood 2-hop
- contradiction candidate lookup

### Concurrency

- single reader
- 10 readers
- 1 writer + 10 readers
- batch writer

## Metrics

### Ingest

- episodes/sec
- facts/sec
- entities/sec
- transaction latency
- rollback latency

### Search

- latency p50/p95/p99
- candidate count
- result count
- ranking duration
- hydration duration

### Storage

- DB file size
- WAL/temp file behavior if visible
- memory usage
- open time
- close time

## CLI

```bash
yeoul bench ingest --episodes 100000 --shape normal --db ./bench.lbug
yeoul bench search --queries ./queries.jsonl --db ./bench.lbug
yeoul bench concurrency --readers 10 --writers 1 --duration 60s
```

## Benchmark output

```json
{
  "benchmark": "ingest",
  "episodes": 100000,
  "duration_sec": 42.1,
  "episodes_per_sec": 2375,
  "db_size_bytes": 123456789,
  "go_version": "go1.22",
  "ladybug_version": "0.11.0"
}
```

## Acceptance baseline

MVP 목표치는 검증 후 조정한다. 초기 목표:

- 10k episode ingest가 local laptop에서 완료되어야 한다.
- recent fact search p95가 500ms 이하가 되도록 목표를 둔다.
- entity lookup p95는 50ms 이하를 목표로 한다.
- neighborhood 2-hop은 result limit이 있을 때 1초 이하를 목표로 한다.

## Regression tracking

Benchmark 결과는 release artifact에 저장한다.

```text
benchmarks/results/2026-04-17.json
```

## 결론

Yeoul benchmark는 “빠르다”를 주장하기 위한 것이 아니라, memory model과 indexing이 실제 local workload에서 유지 가능한지 검증하기 위한 도구다.
