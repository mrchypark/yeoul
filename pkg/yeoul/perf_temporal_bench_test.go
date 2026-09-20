package yeoul

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// H-02 measurement harness.
//
// The hypothesis is that historical (as-of) reads rescan the global revision
// maps once per candidate record, so a query over F facts and E entities costs
// roughly O(F*RF + E*RE) even though each candidate only needs its latest
// revision at one instant.
//
// The fixtures below keep the live record set fixed and grow only the retained
// revision history, so any growth in the measured query isolates the rescan.

const (
	benchHistoricalEntities = 16
	benchHistoricalFacts    = 16
)

type benchHistoricalFixture struct {
	eng             Engine
	asOf            time.Time
	facts           int
	entities        int
	factRevisions   int
	entityRevisions int
}

func newBenchHistoricalEngine(b *testing.B) (Engine, *engine, string) {
	b.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		b.Fatalf("open engine: %v", err)
	}
	b.Cleanup(func() { _ = eng.Close(ctx) })
	raw := eng.(*engine)
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "bench", Content: "historical bench fixture"})
	if err != nil {
		b.Fatalf("ingest episode: %v", err)
	}
	return eng, raw, episode.EpisodeID
}

func (f *benchHistoricalFixture) report(b *testing.B) {
	b.Helper()
	b.ReportMetric(float64(f.facts), "facts")
	b.ReportMetric(float64(f.entities), "entities")
	b.ReportMetric(float64(f.factRevisions), "fact_revisions")
	b.ReportMetric(float64(f.entityRevisions), "entity_revisions")
}

// newBenchFactHistoryFixture asserts a fixed live fact set and then grows only
// the retained fact revision log through a supersede chain. Every cycle appends
// two fact revisions and one replacement fact, while the active fact count
// stays fixed: the superseded facts remain in memory as inactive history, which
// is exactly the "small live database with a long history" shape.
func newBenchFactHistoryFixture(b *testing.B, history int) *benchHistoricalFixture {
	b.Helper()
	ctx := context.Background()
	eng, raw, episodeID := newBenchHistoricalEngine(b)

	entityIDs := make([]string, 0, benchHistoricalEntities)
	for i := 0; i < benchHistoricalEntities; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{
			ID:            fmt.Sprintf("bench:hist:entity:%02d", i),
			Type:          "Bench",
			CanonicalName: fmt.Sprintf("hist-%02d", i),
		})
		if err != nil {
			b.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	tip := ""
	for i := 0; i < benchHistoricalFacts; i++ {
		fact, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("bench:hist:fact:%02d", i),
			Predicate:            "HAS_HISTORY",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("base-%02d", i),
			SupportingEpisodeIDs: []string{episodeID},
		})
		if err != nil {
			b.Fatalf("assert fact %d: %v", i, err)
		}
		if i == 0 {
			tip = fact.ID
		}
	}
	for i := 0; i < history; i++ {
		result, err := eng.SupersedeFact(ctx, tip, FactInput{
			Predicate:            "HAS_HISTORY",
			SubjectID:            entityIDs[0],
			ValueText:            fmt.Sprintf("history-%d", i),
			SupportingEpisodeIDs: []string{episodeID},
		}, "bench history")
		if err != nil {
			b.Fatalf("supersede %d: %v", i, err)
		}
		tip = result.NewFactID
	}
	return &benchHistoricalFixture{
		eng:             eng,
		asOf:            time.Now().UTC().Add(time.Hour),
		facts:           len(raw.facts),
		entities:        len(raw.entities),
		factRevisions:   len(raw.factRevisions),
		entityRevisions: len(raw.entityRevisions),
	}
}

// newBenchEntityHistoryFixture asserts a fixed live fact set and then grows only
// the retained entity revision log by repeatedly updating two entities. The
// entity and fact counts stay fixed, so the measured query isolates the
// per-entity revision rescan.
func newBenchEntityHistoryFixture(b *testing.B, history int) *benchHistoricalFixture {
	b.Helper()
	ctx := context.Background()
	eng, raw, episodeID := newBenchHistoricalEngine(b)

	entityIDs := make([]string, 0, benchHistoricalEntities)
	for i := 0; i < benchHistoricalEntities; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{
			ID:            fmt.Sprintf("bench:hist:entity:%02d", i),
			Type:          "Bench",
			CanonicalName: fmt.Sprintf("hist-%02d", i),
		})
		if err != nil {
			b.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	for i := 0; i < benchHistoricalFacts; i++ {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("bench:hist:fact:%02d", i),
			Predicate:            "HAS_HISTORY",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("base-%02d", i),
			SupportingEpisodeIDs: []string{episodeID},
		}); err != nil {
			b.Fatalf("assert fact %d: %v", i, err)
		}
	}
	for i := 0; i < history; i++ {
		target := entityIDs[i%len(entityIDs)]
		if _, err := eng.UpsertEntity(ctx, EntityInput{
			ID:            target,
			Type:          "Bench",
			CanonicalName: fmt.Sprintf("hist-%02d-v%d", i%len(entityIDs), i),
		}); err != nil {
			b.Fatalf("upsert history %d: %v", i, err)
		}
	}
	return &benchHistoricalFixture{
		eng:             eng,
		asOf:            time.Now().UTC().Add(time.Hour),
		facts:           len(raw.facts),
		entities:        len(raw.entities),
		factRevisions:   len(raw.factRevisions),
		entityRevisions: len(raw.entityRevisions),
	}
}

// BenchmarkHistoricalFactLookupAsOfScaling measures an as-of fact lookup as the
// retained fact revision log grows behind a fixed live fact set.
func BenchmarkHistoricalFactLookupAsOfScaling(b *testing.B) {
	ctx := context.Background()
	for _, history := range []int{100, 400, 1600} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchFactHistoryFixture(b, history)
			req := FactLookupRequest{
				Meta:     QueryMeta{SpaceID: "default"},
				Temporal: TemporalFilter{AsOf: ptrTime(fixture.asOf)},
			}
			if _, err := fixture.eng.LookupFacts(ctx, req); err != nil {
				b.Fatalf("warm lookup: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fixture.eng.LookupFacts(ctx, req); err != nil {
					b.Fatalf("lookup: %v", err)
				}
			}
			b.StopTimer()
			fixture.report(b)
		})
	}
}

// BenchmarkHistoricalNeighborhoodAsOfEntityScaling measures an as-of
// neighborhood query as the retained entity revision log grows behind fixed
// entity and fact counts.
func BenchmarkHistoricalNeighborhoodAsOfEntityScaling(b *testing.B) {
	ctx := context.Background()
	for _, history := range []int{100, 500, 2500} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			fixture := newBenchEntityHistoryFixture(b, history)
			req := NeighborhoodRequest{
				Meta:      QueryMeta{SpaceID: "default"},
				Temporal:  TemporalFilter{AsOf: ptrTime(fixture.asOf)},
				AnchorIDs: []string{"bench:hist:entity:00"},
			}
			if _, err := fixture.eng.Neighborhood(ctx, req); err != nil {
				b.Fatalf("warm neighborhood: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fixture.eng.Neighborhood(ctx, req); err != nil {
					b.Fatalf("neighborhood: %v", err)
				}
			}
			b.StopTimer()
			fixture.report(b)
		})
	}
}
