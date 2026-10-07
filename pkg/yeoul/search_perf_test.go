package yeoul

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func newSearchBenchmarkEngine(b *testing.B) Engine {
	b.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		b.Fatalf("open engine: %v", err)
	}
	b.Cleanup(func() { _ = eng.Close(ctx) })
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "bench", Content: "search benchmark fixture"})
	if err != nil {
		b.Fatalf("ingest episode: %v", err)
	}

	for i := 0; i < 500; i++ {
		entity, err := eng.UpsertEntity(ctx, EntityInput{
			ID:            fmt.Sprintf("search:entity:%04d", i),
			Type:          "Bench",
			CanonicalName: fmt.Sprintf("benchmark entity %04d", i),
		})
		if err != nil {
			b.Fatalf("upsert entity %d: %v", i, err)
		}
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   fmt.Sprintf("search:fact:%04d", i),
			Predicate:            "BENCHMARKS",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("benchmark record %04d with searchable corpus terms", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			b.Fatalf("assert fact %d: %v", i, err)
		}
	}
	return eng
}

func BenchmarkSearchKeyword(b *testing.B) {
	ctx := context.Background()
	eng := newSearchBenchmarkEngine(b)
	req := SearchRequest{QueryText: "searchable corpus", Mode: SearchModeKeyword, Types: []string{"fact", "entity"}}
	if _, err := eng.Search(ctx, req); err != nil {
		b.Fatalf("warm search: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Search(ctx, req); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}

func BenchmarkSearchKeywordNonmatching(b *testing.B) {
	ctx := context.Background()
	eng := newSearchBenchmarkEngine(b)
	req := SearchRequest{QueryText: "searchable absent", Mode: SearchModeKeyword, Types: []string{"fact", "entity"}}
	if _, err := eng.Search(ctx, req); err != nil {
		b.Fatalf("warm search: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Search(ctx, req); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}

func BenchmarkSearchHybrid(b *testing.B) {
	ctx := context.Background()
	eng := newSearchBenchmarkEngine(b)
	req := SearchRequest{QueryText: "searchable corpus", Mode: SearchModeHybrid, Types: []string{"fact", "entity"}}
	if _, err := eng.Search(ctx, req); err != nil {
		b.Fatalf("warm search: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Search(ctx, req); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}

func TestSearchKeywordResponseEquivalence(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer eng.Close(ctx)
	raw := eng.(*engine)
	fixedNow := time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)
	raw.now = func() time.Time { return fixedNow }
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "search:episode",
		Kind:    "note",
		Content: "needle provenance",
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "search:entity",
		Type:          "Test",
		CanonicalName: "search subject",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	for _, id := range []string{"search:fact:a", "search:fact:b"} {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID:                   id,
			Predicate:            "SEARCHES",
			SubjectID:            entity.ID,
			ValueText:            "needle value",
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			t.Fatalf("assert fact %s: %v", id, err)
		}
	}

	response, err := eng.Search(ctx, SearchRequest{
		QueryText: "needle",
		Mode:      SearchModeKeyword,
		Types:     []string{"fact"},
		Include:   Include{SupportingEpisodes: true},
		Page:      Page{Limit: 1},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	wantHits := []SearchHit{{
		HitID:       "hit_search:fact:a",
		HitType:     "fact",
		RecordID:    "search:fact:a",
		Score:       1.4,
		MatchedText: "needle value",
		Reasons:     []string{"keyword_match"},
	}}
	if !reflect.DeepEqual(response.Hits, wantHits) {
		t.Fatalf("hits differ: got %#v, want %#v", response.Hits, wantHits)
	}
	if response.Meta.NextCursor != "offset:1" || response.Meta.SnapshotAt == nil || !response.Meta.SnapshotAt.Equal(fixedNow) {
		t.Fatalf("unexpected response metadata: %+v", response.Meta)
	}
	if len(response.Included.Episodes) != 1 || response.Included.Episodes[0].ID != episode.EpisodeID {
		t.Fatalf("unexpected included records: %+v", response.Included)
	}

	secondPage, err := eng.Search(ctx, SearchRequest{
		QueryText: "needle",
		Mode:      SearchModeKeyword,
		Types:     []string{"fact"},
		Include:   Include{SupportingEpisodes: true},
		Page:      Page{Limit: 1, Cursor: response.Meta.NextCursor},
	})
	if err != nil {
		t.Fatalf("search second page: %v", err)
	}
	wantSecondHit := wantHits[0]
	wantSecondHit.HitID = "hit_search:fact:b"
	wantSecondHit.RecordID = "search:fact:b"
	if !reflect.DeepEqual(secondPage.Hits, []SearchHit{wantSecondHit}) || secondPage.Meta.NextCursor != "" {
		t.Fatalf("unexpected second page: hits=%#v cursor=%q", secondPage.Hits, secondPage.Meta.NextCursor)
	}
	if len(secondPage.Included.Episodes) != 1 || secondPage.Included.Episodes[0].ID != episode.EpisodeID {
		t.Fatalf("unexpected second-page included records: %+v", secondPage.Included)
	}

	noMatch, err := eng.Search(ctx, SearchRequest{
		QueryText: "needle absent",
		Mode:      SearchModeKeyword,
		Types:     []string{"fact"},
	})
	if err != nil {
		t.Fatalf("search nonmatch: %v", err)
	}
	if len(noMatch.Hits) != 0 || noMatch.Meta.NextCursor != "" {
		t.Fatalf("keyword token overlap should not match without a substring: hits=%#v cursor=%q", noMatch.Hits, noMatch.Meta.NextCursor)
	}
}
