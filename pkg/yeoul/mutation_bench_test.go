package yeoul

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// H-01 measurement harness.
//
// The hypothesis is that every mutation clones the whole retained state and
// serializes previous and current records to discover a small delta, so the cost
// of a one-record mutation follows total retained history while the exclusive
// engine lock is held.
//
// Every fixture below therefore holds live cardinality fixed and grows only
// retained history. History is grown through the public API by repeatedly
// upserting one dedicated entity, which appends one entity revision per upsert
// and one entry to that entity's `_history` log, exactly the way a long-lived
// record accumulates history in production.

const (
	benchLiveEntities = 8
	benchLiveFacts    = 24
)

// benchStoreKind selects the storage path a fixture exercises: the production
// LatticeDB store, or the in-memory store that skips persistence entirely.
type benchStoreKind string

const (
	benchStoreLattice  benchStoreKind = "lattice"
	benchStoreInMemory benchStoreKind = "in_memory"
)

// benchMutationFixture holds the engine plus the identity of the record that
// measured mutations touch.
type benchMutationFixture struct {
	eng       *engine
	hotID     string
	hotBase   int
	episode   string
	history   string
	facts     int
	revisions int
}

// newBenchMutationFixture builds a fixed live corpus and `history` extra history
// revisions on top of it.
func newBenchMutationFixture(b *testing.B, kind benchStoreKind, history int) *benchMutationFixture {
	b.Helper()
	ctx := context.Background()
	cfg := Config{InMemory: true}
	if kind == benchStoreLattice {
		cfg = Config{DatabasePath: filepath.Join(b.TempDir(), "bench.db"), CreateIfMissing: true}
	}
	eng, err := Open(ctx, cfg)
	if err != nil {
		b.Fatalf("open engine: %v", err)
	}
	b.Cleanup(func() { _ = eng.Close(ctx) })
	raw := eng.(*engine)

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "bench:episode", Kind: "bench", Content: "bench fixture"})
	if err != nil {
		b.Fatalf("ingest episode: %v", err)
	}
	entityIDs := make([]string, 0, benchLiveEntities)
	for i := 0; i < benchLiveEntities; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{ID: fmt.Sprintf("bench:entity:%03d", i), Type: "Bench", CanonicalName: fmt.Sprintf("live-%03d", i)})
		if err != nil {
			b.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	for i := 0; i < benchLiveFacts; i++ {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("bench:fact:%03d", i),
			Predicate:            "HAS_BENCH_VALUE",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("value-%03d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			b.Fatalf("assert fact %d: %v", i, err)
		}
	}

	historyEntity, err := eng.UpsertEntity(ctx, EntityInput{ID: "bench:entity:history", Type: "Bench", CanonicalName: "history"})
	if err != nil {
		b.Fatalf("upsert history entity: %v", err)
	}
	for i := 0; i < history; i++ {
		if _, err := eng.UpsertEntity(ctx, EntityInput{ID: historyEntity.ID, Type: "Bench", CanonicalName: "history"}); err != nil {
			b.Fatalf("grow history %d: %v", i, err)
		}
	}

	trimBenchEntityHistory(raw, historyEntity.ID, 1)
	return &benchMutationFixture{
		eng:       raw,
		hotID:     entityIDs[0],
		hotBase:   1,
		episode:   episode.EpisodeID,
		history:   historyEntity.ID,
		facts:     benchLiveFacts,
		revisions: len(raw.factRevisions) + len(raw.entityRevisions),
	}
}

// mutateBenchHotEntity performs the measured mutation: one upsert that changes
// exactly one live record and appends one revision.
func mutateBenchHotEntity(ctx context.Context, f *benchMutationFixture) error {
	_, err := f.eng.UpsertEntity(ctx, EntityInput{ID: f.hotID, Type: "Bench", CanonicalName: "live-000"})
	return err
}

// trimBenchEntityHistory resets an entity's `_history` log back to `keep`
// entries. Benchmark iterations call it between timed regions so the next
// measured mutation starts from the same state size; without it the measured
// record would grow by one entry per iteration and the reported per-mutation
// cost would average over a growing state.
func trimBenchEntityHistory(eng *engine, id string, keep int) {
	entity, ok := eng.entities[id]
	if !ok {
		return
	}
	raw, ok := entity.Metadata[entityHistoryKey].([]any)
	if !ok || len(raw) <= keep {
		return
	}
	trimmed := make([]any, keep)
	copy(trimmed, raw[:keep])
	entity.Metadata = mergeAnyMap(entity.Metadata, map[string]any{entityHistoryKey: trimmed})
	eng.entities[id] = entity
}

// BenchmarkMutationUpsertEntityScaling measures one public-API mutation that
// touches a single live record, at a fixed live cardinality and growing retained
// history. The mutation window is also the exclusive-lock window, so these
// numbers bound how long a reader can be blocked by one write.
func BenchmarkMutationUpsertEntityScaling(b *testing.B) {
	ctx := context.Background()
	for _, history := range []int{100, 400, 1600} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchMutationFixture(b, benchStoreLattice, history)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				trimBenchEntityHistory(fixture.eng, fixture.hotID, fixture.hotBase)
				b.StartTimer()
				if err := mutateBenchHotEntity(ctx, fixture); err != nil {
					b.Fatalf("mutate: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(fixture.revisions), "retained_revisions")
		})
	}
}

// BenchmarkMutationUpsertEntityInMemoryScaling runs the same mutation with the
// in-memory store, whose Save is a no-op. Comparing it with the lattice variant
// separates the persistence cost from the engine-side rollback snapshot, since
// both variants pay snapshotLocked and neither pays the store.
func BenchmarkMutationUpsertEntityInMemoryScaling(b *testing.B) {
	ctx := context.Background()
	for _, history := range []int{100, 400, 1600} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchMutationFixture(b, benchStoreInMemory, history)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				trimBenchEntityHistory(fixture.eng, fixture.hotID, fixture.hotBase)
				b.StartTimer()
				if err := mutateBenchHotEntity(ctx, fixture); err != nil {
					b.Fatalf("mutate: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(fixture.revisions), "retained_revisions")
		})
	}
}

// BenchmarkMutationSnapshotCloneScaling isolates the rollback snapshot that
// wraps every mutation: it deep-clones the whole retained state while the
// exclusive lock is held, so its cost sets a floor for every write.
func BenchmarkMutationSnapshotCloneScaling(b *testing.B) {
	for _, history := range []int{100, 400, 1600} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchMutationFixture(b, benchStoreInMemory, history)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if state := fixture.eng.snapshotLocked(); len(state.Facts) != fixture.facts {
					b.Fatalf("unexpected snapshot shape: %d facts", len(state.Facts))
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(fixture.revisions), "retained_revisions")
		})
	}
}

// BenchmarkMutationStoreDeltaScaling isolates the persistence half of the
// hypothesis: the store re-derives every record's payload to find the records a
// mutation actually changed. The timed call persists a one-record delta against
// a state whose size is driven by retained history.
func BenchmarkMutationStoreDeltaScaling(b *testing.B) {
	for _, history := range []int{100, 400, 1600} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchMutationFixture(b, benchStoreLattice, history)
			store := fixture.eng.store
			state := fixture.eng.snapshotLocked()
			if err := store.Save(state); err != nil {
				b.Fatalf("prime store: %v", err)
			}
			tick := time.Now().UTC()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				tick = tick.Add(time.Second)
				hot := state.Entities[fixture.hotID]
				hot.UpdatedAt = tick
				state.Entities[fixture.hotID] = hot
				b.StartTimer()
				if err := store.Save(state); err != nil {
					b.Fatalf("save delta: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(fixture.revisions), "retained_revisions")
		})
	}
}
