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

// countTemporalIndexRebuildsByKind runs fn and reports how many rebuilds
// scanned each revision kind, so a test can prove a single-kind read never
// touches the other kind's revision log.
func countTemporalIndexRebuildsByKind(fn func()) map[string]int {
	previousTotal := temporalIndexRebuildObserver
	previousKind := temporalIndexKindObserver
	byKind := map[string]int{}
	temporalIndexRebuildObserver = func() {}
	temporalIndexKindObserver = func(kind string) { byKind[kind]++ }
	defer func() {
		temporalIndexRebuildObserver = previousTotal
		temporalIndexKindObserver = previousKind
	}()
	fn()
	return byKind
}

// TestAsOfFactLookupBuildsRevisionIndexOnce guards the H-02 fix: an as-of fact
// lookup must derive the newest revision at the requested instant once for the
// whole query, not rescan the global revision log for every candidate fact.
//
// The regression it guards against is quadratic in retained history, so the
// measured quantity is the number of scans rather than the query result: a
// per-candidate rescan performs one scan per live fact plus one per supporting
// entity, while the fixed path performs exactly one.
// TestSingleKindAsOfReadsDoNotScanTheOtherRevisionLog guards the cross-domain
// coupling a combined temporal index would introduce: a fact-only as-of read
// must not scan the entity revision log, and vice versa.
//
// The regression is invisible to the scaling benchmarks, which grow one
// history while holding the other fixed. A database with a tiny fact history
// and a huge entity history would make a fact lookup proportional to unrelated
// entity history, so the guard asserts on scans by kind rather than on time.
func TestSingleKindAsOfReadsDoNotScanTheOtherRevisionLog(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "kind isolation guard"})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	// Skew the histories: one entity revision against many fact revisions,
	// then the inverse for the entity-only read.
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "iso:entity", Type: "Thing", CanonicalName: "iso"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	const factCount = 40
	for i := 0; i < factCount; i++ {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("iso:fact:%02d", i),
			Predicate:            "HAS_ISO",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("iso-value-%02d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			t.Fatalf("assert fact %d: %v", i, err)
		}
	}

	asOf := time.Now().UTC().Add(time.Hour)
	factReq := FactLookupRequest{
		Meta:     QueryMeta{SpaceID: "default"},
		Temporal: TemporalFilter{AsOf: &asOf},
	}
	factScans := countTemporalIndexRebuildsByKind(func() {
		if _, err := eng.LookupFacts(ctx, factReq); err != nil {
			t.Fatalf("fact lookup: %v", err)
		}
	})
	if factScans["entity"] != 0 {
		t.Fatalf("fact-only as-of lookup scanned the entity revision log %d times, want 0 (scans by kind: %v)",
			factScans["entity"], factScans)
	}
	if factScans["fact"] == 0 {
		t.Fatalf("fact-only as-of lookup never indexed the fact revision log (scans by kind: %v)", factScans)
	}

	// GetRecord resolves a single record, so it has no reason to inspect the
	// opposite revision log at all.
	recordScans := countTemporalIndexRebuildsByKind(func() {
		if _, err := eng.GetRecord(ctx, GetRecordRequest{
			Meta:     QueryMeta{SpaceID: "default"},
			Kind:     "fact",
			ID:       "iso:fact:00",
			Temporal: TemporalFilter{AsOf: &asOf},
		}); err != nil {
			t.Fatalf("get record: %v", err)
		}
	})
	if recordScans["entity"] != 0 {
		t.Fatalf("single fact GetRecord scanned the entity revision log %d times, want 0 (scans by kind: %v)",
			recordScans["entity"], recordScans)
	}

	entityScans := countTemporalIndexRebuildsByKind(func() {
		if _, err := eng.GetRecord(ctx, GetRecordRequest{
			Meta:     QueryMeta{SpaceID: "default"},
			Kind:     "entity",
			ID:       entity.ID,
			Temporal: TemporalFilter{AsOf: &asOf},
		}); err != nil {
			t.Fatalf("get entity record: %v", err)
		}
	})
	if entityScans["fact"] != 0 {
		t.Fatalf("single entity GetRecord scanned the fact revision log %d times, want 0 (scans by kind: %v)",
			entityScans["fact"], entityScans)
	}
}

// TestFactLookupScalingIsIndependentOfEntityHistory is the cross-cardinality
// guard for the same property, measured rather than observed: a fact-only
// as-of read must not grow with unrelated entity history.
func TestFactLookupScalingIsIndependentOfEntityHistory(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "cross cardinality guard"})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	subject, err := eng.UpsertEntity(ctx, EntityInput{ID: "xc:subject", Type: "Thing", CanonicalName: "xc"})
	if err != nil {
		t.Fatalf("upsert subject: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID:                   "xc:fact",
		Predicate:            "HAS_XC",
		SubjectID:            subject.ID,
		ValueText:            "xc-value",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	// Grow entity history well past the single fact history.
	const entityCount = 200
	for i := 0; i < entityCount; i++ {
		if _, err := eng.UpsertEntity(ctx, EntityInput{
			ID:            fmt.Sprintf("xc:entity:%03d", i),
			Type:          "Thing",
			CanonicalName: fmt.Sprintf("xc-%03d", i),
		}); err != nil {
			t.Fatalf("upsert entity %d: %v", i, err)
		}
	}

	asOf := time.Now().UTC().Add(time.Hour)
	scans := countTemporalIndexRebuildsByKind(func() {
		if _, err := eng.LookupFacts(ctx, FactLookupRequest{
			Meta:     QueryMeta{SpaceID: "default"},
			Temporal: TemporalFilter{AsOf: &asOf},
		}); err != nil {
			t.Fatalf("fact lookup: %v", err)
		}
	})
	if scans["entity"] != 0 {
		t.Fatalf("fact-only as-of lookup scanned %d entity revisions, want 0: a fact read must not be proportional to entity history (scans by kind: %v)",
			entityCount, scans)
	}
}

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
	// A neighborhood walks entities and facts, so it indexes both kinds - once
	// each. The guard is that neither kind is rescanned per candidate record,
	// not that the query touches a single kind.
	rebuilds := countTemporalIndexRebuildsByKind(func() {
		var err error
		resp, err = eng.Neighborhood(ctx, req)
		if err != nil {
			t.Fatalf("neighborhood: %v", err)
		}
	})
	if len(resp.Nodes) == 0 {
		t.Fatal("expected the as-of neighborhood to return nodes")
	}
	if rebuilds["fact"] != 1 || rebuilds["entity"] != 1 {
		t.Fatalf("as-of neighborhood over %d entities rebuilt the revision index %v, want exactly one rebuild per kind", entityCount, rebuilds)
	}
}
