package yeoul

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// countTemporalIndexRebuilds runs fn and returns how many times the temporal
// index scanned the global revision maps while it ran.
func countTemporalIndexRebuilds(fn func()) int {
	previous := temporalIndexRebuildObserver
	rebuilds := 0
	temporalIndexRebuildObserver = func() { rebuilds++ }
	defer func() { temporalIndexRebuildObserver = previous }()
	fn()
	return rebuilds
}

// TestAsOfFactLookupBuildsRevisionIndexOnce guards the H-02 fix: an as-of fact
// lookup must derive the newest revision at the requested instant once for the
// whole query, not rescan the global revision log for every candidate fact.
//
// The regression it guards against is quadratic in retained history, so the
// measured quantity is the number of scans rather than the query result: a
// per-candidate rescan performs one scan per live fact plus one per supporting
// entity, while the fixed path performs exactly one.
func TestAsOfFactLookupBuildsRevisionIndexOnce(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "index guard"})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	const entityCount = 12
	entityIDs := make([]string, 0, entityCount)
	for i := 0; i < entityCount; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{ID: fmt.Sprintf("guard:entity:%02d", i), Type: "Thing", CanonicalName: fmt.Sprintf("guard-%02d", i)})
		if err != nil {
			t.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	const factCount = 24
	for i := 0; i < factCount; i++ {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("guard:fact:%02d", i),
			Predicate:            "HAS_GUARD",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("guard-value-%02d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			t.Fatalf("assert fact %d: %v", i, err)
		}
	}

	asOf := time.Now().UTC().Add(time.Hour)
	req := FactLookupRequest{
		Meta:     QueryMeta{SpaceID: "default"},
		Temporal: TemporalFilter{AsOf: &asOf},
	}
	var resp *FactLookupResponse
	rebuilds := countTemporalIndexRebuilds(func() {
		var err error
		resp, err = eng.LookupFacts(ctx, req)
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
	})
	if len(resp.Facts) != factCount {
		t.Fatalf("expected the as-of lookup to visit all %d facts, got %d", factCount, len(resp.Facts))
	}
	if rebuilds != 1 {
		t.Fatalf("as-of lookup over %d facts rescanned the revision index %d times, want exactly 1; the temporal index is being rebuilt per candidate record",
			factCount, rebuilds)
	}
}

// TestAsOfNeighborhoodBuildsRevisionIndexOnce guards the same property for the
// neighborhood path, whose entity and fact candidates would otherwise each pay
// their own rescan of the entity and fact revision logs.
func TestAsOfNeighborhoodBuildsRevisionIndexOnce(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "neighborhood guard"})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	const entityCount = 10
	entityIDs := make([]string, 0, entityCount)
	for i := 0; i < entityCount; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{ID: fmt.Sprintf("guard:nb:%02d", i), Type: "Thing", CanonicalName: fmt.Sprintf("nb-%02d", i)})
		if err != nil {
			t.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	for i := 0; i < 20; i++ {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("guard:nb:fact:%02d", i),
			Predicate:            "HAS_GUARD",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("nb-value-%02d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			t.Fatalf("assert fact %d: %v", i, err)
		}
	}

	asOf := time.Now().UTC().Add(time.Hour)
	req := NeighborhoodRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		Temporal:  TemporalFilter{AsOf: &asOf},
		AnchorIDs: []string{entityIDs[0]},
	}
	var resp *NeighborhoodResponse
	rebuilds := countTemporalIndexRebuilds(func() {
		var err error
		resp, err = eng.Neighborhood(ctx, req)
		if err != nil {
			t.Fatalf("neighborhood: %v", err)
		}
	})
	if len(resp.Nodes) == 0 {
		t.Fatal("expected the as-of neighborhood to return nodes")
	}
	if rebuilds != 1 {
		t.Fatalf("as-of neighborhood over %d entities rescanned the revision index %d times, want exactly 1", entityCount, rebuilds)
	}
}
