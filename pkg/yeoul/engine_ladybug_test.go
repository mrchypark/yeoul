package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
)

func TestLadybugDriverReopenPersistence(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.lbug")

	eng, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open ladybug engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "chat_message",
		Content: "ladybug persistence smoke test",
		Source: SourceInput{
			Kind:        "chat",
			ExternalRef: "thread-ladybug",
		},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	project, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	if _, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "HAS_STATE",
		SubjectID:            project.ID,
		ValueText:            "stored-in-ladybug",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	reopened, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("reopen ladybug engine: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()

	search, err := reopened.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "stored-in-ladybug",
	})
	if err != nil {
		t.Fatalf("search reopened engine: %v", err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("expected persisted search hits after reopen")
	}
}

func TestLadybugDriverPersistsGraphNativeRecords(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-graph.lbug")

	eng, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open ladybug engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "Yeoul uses Ladybug.",
		Source: SourceInput{
			Kind:        "chat",
			ExternalRef: "thread-graph",
		},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	project, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	storage, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Database",
		CanonicalName: "Ladybug",
	})
	if err != nil {
		t.Fatalf("upsert storage: %v", err)
	}

	fact, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "USES_STORAGE_ENGINE",
		SubjectID:            project.ID,
		ObjectID:             storage.ID,
		ValueText:            "Yeoul uses Ladybug.",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	raw, err := lstore.Open(dbPath, true)
	if err != nil {
		t.Fatalf("open raw ladybug store: %v", err)
	}
	defer raw.Close()

	assertSingleStringResult(t, raw, "MATCH (f:Fact {id: '"+fact.ID+"'}) RETURN f.predicate", "USES_STORAGE_ENGINE")
	assertSingleStringResult(t, raw, "MATCH (f:Fact {id: '"+fact.ID+"'})-[:SUBJECT]->(e:Entity) RETURN e.canonical_name", "Yeoul")
	assertSingleStringResult(t, raw, "MATCH (f:Fact {id: '"+fact.ID+"'})-[:OBJECT_ENTITY]->(e:Entity) RETURN e.canonical_name", "Ladybug")
	assertSingleStringResult(t, raw, "MATCH (f:Fact {id: '"+fact.ID+"'})-[:SUPPORTED_BY]->(ep:Episode) RETURN ep.id", episode.EpisodeID)
}

func TestLadybugDriverReadOnlyOpenAndWriteBlock(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-readonly.lbug")

	writable, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open writable engine: %v", err)
	}
	if _, err := writable.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "seed",
		Source:  SourceInput{Kind: "note"},
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if err := writable.Close(ctx); err != nil {
		t.Fatalf("close writable engine: %v", err)
	}

	readonly, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
		ReadOnly:     true,
	})
	if err != nil {
		t.Fatalf("open readonly engine: %v", err)
	}
	defer func() { _ = readonly.Close(ctx) }()

	search, err := readonly.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "seed",
	})
	if err != nil {
		t.Fatalf("readonly search: %v", err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("expected readonly search hit")
	}

	_, err = readonly.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "blocked",
		Source:  SourceInput{Kind: "note"},
	})
	if err == nil {
		t.Fatal("expected readonly write error")
	}

	var yeErr *Error
	if !errors.As(err, &yeErr) {
		t.Fatalf("expected structured error, got %T", err)
	}
	if yeErr.Code != ErrNotSupported {
		t.Fatalf("unexpected error code: %s", yeErr.Code)
	}
}

func TestLadybugDriverPreservesSequenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-sequence.lbug")

	eng, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	first, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "first",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest first: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close first engine: %v", err)
	}

	reopened, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()

	second, err := reopened.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "second",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest second: %v", err)
	}
	if second.EpisodeID == first.EpisodeID {
		t.Fatalf("expected distinct episode ids across reopen, got %q", second.EpisodeID)
	}
}

func TestLadybugDriverIncrementalWritesDoNotDuplicateNodesOrEdges(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-incremental.lbug")

	eng, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-1",
		SpaceID: "default",
		Kind:    "note",
		Content: "Yeoul uses Ladybug.",
		Source:  SourceInput{Kind: "note", ExternalRef: "thread-inc"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	project, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "project:yeoul",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	storage, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "database:ladybug",
		SpaceID:       "default",
		Type:          "Database",
		CanonicalName: "Ladybug",
	})
	if err != nil {
		t.Fatalf("upsert storage: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-1",
		SpaceID:              "default",
		Predicate:            "USES_STORAGE_ENGINE",
		SubjectID:            project.ID,
		ObjectID:             storage.ID,
		ValueText:            "Yeoul uses Ladybug.",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "project:yeoul",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
		Aliases:       []string{"여울"},
	}); err != nil {
		t.Fatalf("update project aliases: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	raw, err := lstore.Open(dbPath, true)
	if err != nil {
		t.Fatalf("open raw ladybug store: %v", err)
	}
	defer raw.Close()

	assertRowCount(t, raw, "MATCH (e:Entity {id: 'project:yeoul'}) RETURN e.id", 1)
	assertRowCount(t, raw, "MATCH (:Fact {id: 'fact-1'})-[:SUPPORTED_BY]->(:Episode {id: 'ep-1'}) RETURN 1", 1)
	assertRowCount(t, raw, "MATCH (:Episode {id: 'ep-1'})-[:ASSERTS]->(:Fact {id: 'fact-1'}) RETURN 1", 1)
}

func assertSingleStringResult(t *testing.T, store *lstore.Store, query, want string) {
	t.Helper()

	result, err := store.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer result.Close()

	if !result.HasNext() {
		t.Fatalf("query %q returned no rows", query)
	}
	tuple, err := result.Next()
	if err != nil {
		t.Fatalf("next row for %q: %v", query, err)
	}
	value, err := tuple.GetValue(0)
	if err != nil {
		t.Fatalf("get value for %q: %v", query, err)
	}
	got, ok := value.(string)
	if !ok {
		t.Fatalf("query %q returned non-string value %#v", query, value)
	}
	if got != want {
		t.Fatalf("query %q returned %q, want %q", query, got, want)
	}
}

func assertRowCount(t *testing.T, store *lstore.Store, query string, want int) {
	t.Helper()

	result, err := store.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer result.Close()

	got := 0
	for result.HasNext() {
		if _, err := result.Next(); err != nil {
			t.Fatalf("next row for %q: %v", query, err)
		}
		got++
	}
	if got != want {
		t.Fatalf("query %q returned %d rows, want %d", query, got, want)
	}
}
