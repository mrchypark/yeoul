package yeoul

import (
	"context"
	"testing"
	"time"
)

// TestCardinalityOneBoundedReplacementDropsActiveSuffix pins the documented
// consequence of cardinality-one slot replacement: the whole overlapping fact
// is superseded, so replacing an open-ended [January, infinity) fact with a
// bounded [February, March) fact does not preserve an active suffix after
// March. January still resolves historically, February resolves to the
// replacement, and April resolves to nothing in either inactive setting.
func TestCardinalityOneBoundedReplacementDropsActiveSuffix(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "bounded replacement", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "bounded replacement"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	jan := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	january, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:bounded-jan",
		Predicate:            "HAS_STATE",
		SubjectID:            entity.ID,
		ValueText:            "january",
		ValidFrom:            jan,
		Cardinality:          factCardinalityOne,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert january: %v", err)
	}
	february, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:bounded-feb",
		Predicate:            "HAS_STATE",
		SubjectID:            entity.ID,
		ValueText:            "february",
		ValidFrom:            feb,
		ValidTo:              mar,
		Cardinality:          factCardinalityOne,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert february: %v", err)
	}

	storedJanuary, err := eng.GetFact(ctx, january.ID)
	if err != nil {
		t.Fatalf("get january: %v", err)
	}
	if storedJanuary.Status != factStatusSuperseded {
		t.Fatalf("expected the open-ended January fact to be wholly superseded, got %q", storedJanuary.Status)
	}
	if !storedJanuary.ValidTo.Equal(feb) {
		t.Fatalf("expected January validity clipped to February, got %v", storedJanuary.ValidTo)
	}

	lookup := func(label string, at time.Time, includeInactive bool) []string {
		t.Helper()
		instant := at
		res, err := eng.LookupFacts(ctx, FactLookupRequest{
			SubjectIDs: []string{entity.ID},
			Temporal:   TemporalFilter{ValidAt: &instant, IncludeInactive: includeInactive},
		})
		if err != nil {
			t.Fatalf("lookup %s: %v", label, err)
		}
		ids := make([]string, 0, len(res.Facts))
		for _, fact := range res.Facts {
			ids = append(ids, fact.ID)
		}
		return ids
	}

	janQuery := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	febQuery := time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC)
	aprQuery := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)

	if got := lookup("january active-only", janQuery, false); len(got) != 0 {
		t.Fatalf("expected no active fact in January, got %v", got)
	}
	if got := lookup("january with inactive", janQuery, true); len(got) != 1 || got[0] != january.ID {
		t.Fatalf("expected only the superseded January fact in January, got %v", got)
	}
	if got := lookup("february active-only", febQuery, false); len(got) != 1 || got[0] != february.ID {
		t.Fatalf("expected the February replacement in February, got %v", got)
	}
	if got := lookup("february with inactive", febQuery, true); len(got) != 1 || got[0] != february.ID {
		t.Fatalf("expected only the February replacement in February, got %v", got)
	}
	if got := lookup("april active-only", aprQuery, false); len(got) != 0 {
		t.Fatalf("expected no active fact in April after a bounded replacement, got %v", got)
	}
	if got := lookup("april with inactive", aprQuery, true); len(got) != 0 {
		t.Fatalf("expected no fact at all in April, got %v", got)
	}
}
