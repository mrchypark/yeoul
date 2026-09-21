# Performance Hypothesis Measurements

Three performance hypotheses from the whole-repo deep review were labeled
"measure first": the required outcome was a credible measurement plus either a
scoped fix or documented evidence. This document records the benchmark, the
numbers, and the conclusion for each. All numbers were taken on Apple M1 (8 P),
Go 1.27, with the benchmarks in this repository.

Reproduce with:

```sh
go test ./pkg/yeoul/ -run '^$' -bench 'BenchmarkMutation' -benchtime=50x
go test ./pkg/yeoul/ -run '^$' -bench 'BenchmarkHistorical' -benchtime=100x
```

## H-01: mutation clones full state and serializes previous/current records

**Benchmark:** `BenchmarkMutationUpsertEntityScaling` in
`pkg/yeoul/mutation_bench_test.go`, supported by
`BenchmarkMutationUpsertEntityInMemoryScaling`,
`BenchmarkMutationSnapshotCloneScaling`, and `BenchmarkMutationStoreDeltaScaling`.
The fixture holds live cardinality fixed at 8 entities and 24 facts and grows
only retained history, so the measured quantity is the cost of one public-API
upsert that changes exactly one live record.

**Confirmed.** The mutation is linear in total retained history, and the store's
delta discovery dominated it. Before, `UpsertEntityScaling` at 133 / 433 / 1633
retained revisions cost 1.08 ms / 2.14 ms / 7.47 ms per mutation (9.4 KB / 2.15
MB / 6.57 MB allocated), so a 12x history growth cost about 6.9x time while the
live record set never changed. The store half alone (`StoreDeltaScaling`) was
1.64 ms / 4.02 ms / 4.80 ms, and the rollback clone
(`SnapshotCloneScaling`) was 33 us / 130 us / 718 us. The mutation runs under the
exclusive engine lock, so these numbers also bound how long one write blocks a
reader.

The mechanism was in `latticeStore.Save`: it compared `prev` and `state` with
`reflect.DeepEqual` and then `reconcileNodes` marshalled **every record in both
states** with `json.Marshal` to discover which records changed. Serializing the
whole retained history to find a one-record delta is what made a small write
follow total history.

**Fix:** `Save` now computes `latticeRecordDelta(prev, state)`, which compares
record values directly and marshals only the records that were added, changed,
or removed. Unchanged records cost a comparison and no allocation.
`reconcileNodes` takes that delta instead of both states, and the no-op check
still uses `reflect.DeepEqual` so the skip behavior is unchanged. No public API
or persisted format changed.

| benchmark | history | before | after |
| --- | --- | --- | --- |
| `UpsertEntityScaling` | 133 rev | 1,083,083 ns | 630,000 ns |
| `UpsertEntityScaling` | 433 rev | 2,140,862 ns | 1,432,000 ns |
| `UpsertEntityScaling` | 1633 rev | 7,472,073 ns | 5,239,000 ns |
| `StoreDeltaScaling` | 133 rev | 1,638,393 ns | 294,414 ns |
| `StoreDeltaScaling` | 433 rev | 4,015,419 ns | 482,971 ns |
| `StoreDeltaScaling` | 1633 rev | 4,801,431 ns | 2,051,078 ns |

Allocations for the store pass fell from 580 KB / 1.48 MB / 4.66 MB to 225 KB /
514 KB / 1.67 MB. The residual growth is the one unavoidable serialize of the
state being written plus the engine's rollback clone; the full double
serialization is gone. The remaining engine-side `snapshotLocked` clone is
measured separately by `SnapshotCloneScaling` and was left alone: it is the
rollback snapshot the mutation contract requires.

## H-02: historical queries rescan global revision maps per candidate

**Benchmark:** `BenchmarkHistoricalFactLookupAsOfScaling` and
`BenchmarkHistoricalNeighborhoodAsOfEntityScaling` in
`pkg/yeoul/perf_temporal_bench_test.go`. Both hold the live record set fixed and
grow only retained revision history.

**Confirmed, and partly already addressed.** PR #126 (`863893a`) already fixed
the timeline lifecycle half: `factRevisionsByFact()` indexes the revision log
once, guarded by `TestTimelineLifecycleDerivationStaysLinearInRevisions`. The
remaining cost was in `latestFactRevisionAt` / `latestEntityRevisionAt`, which
scanned the whole global revision map for every candidate record, giving the
hypothesized `O(F x RF + E x RE)`.

Before, `FactLookupAsOfScaling` at 316 / 1216 / 4816 fact revisions cost 1.02 ms /
14.5 ms / 329 ms, which is quadratic in retained history (15x history cost 320x
time). `NeighborhoodAsOfEntityScaling` at 116 / 516 / 2516 entity revisions cost
68 us / 250 us / 822 us.

**Fix:** a query-local `temporalIndex` (`pkg/yeoul/temporal_index.go`) resolves
the newest revision at the requested instant once per query, and the helpers
look the answer up per candidate. `factVersionAt`, `entityVersionAt`,
`matchesScopeForFact`, `addFactSupport`, and `searchCorpusStats` take the index.
The old per-record `latestFactRevisionAt` / `latestEntityRevisionAt` scans were
deleted. The index is scratch held under the read lock and dropped when the
query returns, so nothing is cached on the engine and no mutation has to
invalidate it.

| benchmark | history | before | after |
| --- | --- | --- | --- |
| `FactLookupAsOfScaling` | 316 rev | 1,016,372 ns | 252,330 ns |
| `FactLookupAsOfScaling` | 1216 rev | 14,546,122 ns | 669,323 ns |
| `FactLookupAsOfScaling` | 4816 rev | 329,489,216 ns | 4,695,472 ns |
| `NeighborhoodAsOfEntityScaling` | 116 rev | 67,821 ns | 31,196 ns |
| `NeighborhoodAsOfEntityScaling` | 516 rev | 249,920 ns | 70,878 ns |
| `NeighborhoodAsOfEntityScaling` | 2516 rev | 821,877 ns | 170,443 ns |

The largest fact-lookup point improved about 70x and the growth is now close to
linear in revisions rather than quadratic. The residual cost is the per-request
index build plus candidate resolution, which the fix intentionally does not
cache.

**Guard:** `TestAsOfFactLookupBuildsRevisionIndexOnce` and
`TestAsOfNeighborhoodBuildsRevisionIndexOnce` in `pkg/yeoul/temporal_index_test.go`
use the `temporalIndexRebuildObserver` hook to assert that one as-of query
scans the revision index exactly once. The old per-candidate rescan would scan
once per live record and fail this immediately.

## H-03: primary Rax retrieval still runs a full core search

**Historical record.** The benchmarks in `cmd/yeoul/rax_primary_bench_test.go` confirmed that primary Rax retrieval ran a full core search to obtain rerank scores, so it did not reduce core CPU. The measured integration has since been removed from the product: Yeoul no longer ships or resolves an external retrieval runtime. This finding is why no external retrieval runtime is shipped.

## Summary

| hypothesis | confirmed | action |
| --- | --- | --- |
| H-01 mutation clones and serializes full state | yes | fixed; store pass 2.3x-8.3x faster |
| H-02 historical queries rescan revision maps | yes (timeline half already fixed by #126) | fixed; up to ~70x faster, now linear |
| H-03 primary Rax still runs a full core search | yes | confirmed; integration removed, no external retrieval runtime shipped |
