package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	latticedb "github.com/mrchypark/latticedb-go"
	lstore "github.com/mrchypark/yeoul/internal/storage/lattice"
)

func TestNativeFullTextSearchPersistsAndFiltersBeforePagination(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fulltext.ltdb")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		space, group, kind := "other", "wanted", "chat"
		if i%2 == 1 {
			space, group, kind = "default", "unwanted", "note"
		}
		_, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: "distractor-" + string(rune('a'+i)), SpaceID: space, GroupID: group,
			Kind: "note", Content: "alpha unrelated wording beta",
			Source: SourceInput{Kind: kind, ExternalRef: "distractor"},
		})
		if err != nil {
			t.Fatalf("ingest distractor %d: %v", i, err)
		}
	}
	for _, id := range []string{"z-good", "a-good"} {
		_, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: id, GroupID: "wanted", Kind: "note", Content: "alpha unrelated wording beta",
			Source: SourceInput{Kind: "chat", ExternalRef: id},
		})
		if err != nil {
			t.Fatalf("ingest wanted episode %s: %v", id, err)
		}
	}
	keyword, err := eng.Search(ctx, SearchRequest{QueryText: "alpha beta", Mode: SearchModeKeyword, Types: []string{"episode"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(keyword.Hits) != 0 {
		t.Fatalf("keyword unexpectedly matched non-contiguous terms: %#v", keyword.Hits)
	}
	req := SearchRequest{
		QueryText: "alpha beta", Mode: SearchModeFullText, Types: []string{"episode"},
		Scope: ScopeFilter{GroupIDs: []string{"wanted"}, SourceKinds: []string{"chat"}},
		Page:  Page{Limit: 1},
	}
	first, err := eng.Search(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(first.Hits); !reflect.DeepEqual(got, []string{"a-good"}) || first.Meta.NextCursor == "" {
		t.Fatalf("first filtered page = %v cursor=%q", got, first.Meta.NextCursor)
	}
	req.Page.Cursor = first.Meta.NextCursor
	second, err := eng.Search(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(second.Hits); !reflect.DeepEqual(got, []string{"z-good"}) || second.Meta.NextCursor != "" {
		t.Fatalf("second filtered page = %v cursor=%q", got, second.Meta.NextCursor)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}

	readOnly, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen read-only: %v", err)
	}
	defer readOnly.Close(ctx)
	got, err := readOnly.Search(ctx, SearchRequest{
		QueryText: "alpha beta", Mode: SearchModeFullText, Types: []string{"episode"},
		Scope: ScopeFilter{GroupIDs: []string{"wanted"}, SourceKinds: []string{"chat"}},
	})
	if err != nil || !reflect.DeepEqual(hitIDs(got.Hits), []string{"a-good", "z-good"}) {
		t.Fatalf("persisted fulltext search: hits=%v err=%v", hitIDs(got.Hits), err)
	}
}

func TestFullTextMissingDefinitionIsRepairedOnWritableOpen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "missing-definition.ltdb")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "repair-me", Kind: "note", Content: "repair token"}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DropFTSIndex("yeoul_episode_search_v1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	eng, err = Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice})
	if err != nil {
		t.Fatalf("writable open repairs missing definition: %v", err)
	}
	defer eng.Close(ctx)
	got, err := eng.Search(ctx, SearchRequest{QueryText: "repair token", Mode: SearchModeFullText, Types: []string{"episode"}})
	if err != nil || !reflect.DeepEqual(hitIDs(got.Hits), []string{"repair-me"}) {
		t.Fatalf("search after repair: hits=%v err=%v", hitIDs(got.Hits), err)
	}
}

func TestNativeFullTextTracksUpdatedSearchText(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: filepath.Join(t.TempDir(), "updated.ltdb"), Driver: StorageDriverLattice, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "mutable-entity", Type: "Project", CanonicalName: "obsolete token"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := eng.UpsertEntity(ctx, EntityInput{ID: entity.ID, Type: "Project", CanonicalName: "current token"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Search(ctx, SearchRequest{QueryText: "obsolete", Mode: SearchModeFullText, Types: []string{"entity"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) != 0 {
		t.Fatalf("obsolete term remained indexed: hits=%v", hitIDs(got.Hits))
	}
	got, err = eng.Search(ctx, SearchRequest{QueryText: "current", Mode: SearchModeFullText, Types: []string{"entity"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hitIDs(got.Hits), []string{updated.ID}) {
		t.Fatalf("updated term not indexed: hits=%v", hitIDs(got.Hits))
	}
}

func TestFullTextAsOfRejectsWithoutChangingKeywordHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "history.ltdb")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	concrete := eng.(*engine)
	knownAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	concrete.now = func() time.Time { return knownAt }
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "history-subject", Type: "Project", CanonicalName: "History"})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "history-source", Kind: "note", Content: "fact provenance"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{ID: "history-fact", Predicate: "HAS_NOTE", SubjectID: entity.ID, ValueText: "retired comet term", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.SupersedeFact(ctx, fact.ID, FactInput{
		ID: "history-fact-new", Predicate: fact.Predicate, SubjectID: entity.ID,
		ValueText: "current replacement", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "updated")
	if err != nil {
		t.Fatal(err)
	}
	historical := SearchRequest{QueryText: "retired comet", Mode: SearchModeKeyword, Types: []string{"fact"}, Temporal: TemporalFilter{AsOf: &knownAt}}
	got, err := eng.Search(ctx, historical)
	if err != nil || !reflect.DeepEqual(hitIDs(got.Hits), []string{fact.ID}) {
		t.Fatalf("historical keyword search: hits=%v err=%v", hitIDs(got.Hits), err)
	}
	historical.Mode = SearchModeFullText
	if _, err := eng.Search(ctx, historical); !hasErrorCode(err, ErrNotSupported) {
		t.Fatalf("fulltext as_of error = %v, want not-supported", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFullTextReadOnlyWithoutReadyIndexIsNotSupported(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "unindexed.ltdb")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "unindexed", Kind: "note", Content: "legacy searchable"}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *latticedb.Tx) error {
		return tx.DeleteAppMetadata([]byte("yeoul.search.fts.version"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverLattice, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close(ctx)
	_, err = readOnly.Search(ctx, SearchRequest{QueryText: "legacy searchable", Mode: SearchModeFullText, Types: []string{"episode"}})
	if !hasErrorCode(err, ErrNotSupported) || !errors.Is(err, lstore.ErrSearchIndexUnavailable) {
		t.Fatalf("unindexed read-only search error = %v", err)
	}
}

func TestRecordMatchesQueryDoesNotApproximateFullText(t *testing.T) {
	record := Episode{Content: "alpha unrelated wording beta"}
	if RecordMatchesQuery(SearchModeFullText, "alpha beta", record) {
		t.Fatal("record-only matcher must not approximate native full-text semantics")
	}
}

func hitIDs(hits []SearchHit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.RecordID
	}
	return ids
}
