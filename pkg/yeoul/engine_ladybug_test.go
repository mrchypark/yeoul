package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func TestLadybugDriverPersistsBitemporalRevisions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-revisions.lbug")

	eng, err := Open(ctx, Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-rev",
		SpaceID: "default",
		Kind:    "note",
		Content: "status changed",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	project, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "project:rev",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Revisioned",
	})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-rev-old",
		SpaceID:              "default",
		Predicate:            "HAS_STATUS",
		SubjectID:            project.ID,
		ValueText:            "alpha",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}
	beforeSupersede := oldFact.CreatedAt.Add(time.Nanosecond)
	transition, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID:                   "fact-rev-new",
		SpaceID:              "default",
		Predicate:            "HAS_STATUS",
		SubjectID:            project.ID,
		ValueText:            "beta",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "status update")
	if err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	raw, err := lstore.Open(dbPath, true)
	if err != nil {
		t.Fatalf("open raw ladybug store: %v", err)
	}
	assertRowCount(t, raw, "MATCH (m:YeoulMigration {id: 'bitemporal_revision_seed_v1'}) RETURN m.id", 1)
	assertRowCount(t, raw, "MATCH (r:EntityRevision {entity_id: 'project:rev'}) RETURN r.id", 1)
	assertRowCount(t, raw, "MATCH (r:FactRevision {fact_id: 'fact-rev-old'}) RETURN r.id", 2)
	assertRowCount(t, raw, "MATCH (r:FactRevision {fact_id: 'fact-rev-new'}) RETURN r.id", 2)
	raw.Close()

	reopened, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()

	historical, err := reopened.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
		Temporal:   TemporalFilter{AsOf: &beforeSupersede},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Facts) != 1 || historical.Facts[0].ID != oldFact.ID || historical.Facts[0].ValueText != "alpha" {
		t.Fatalf("expected old fact after reopen, got %#v", historical.Facts)
	}

	current, err := reopened.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
	})
	if err != nil {
		t.Fatalf("current lookup: %v", err)
	}
	if len(current.Facts) != 1 || current.Facts[0].ID != transition.NewFactID || current.Facts[0].ValueText != "beta" {
		t.Fatalf("expected new current fact after reopen, got %#v", current.Facts)
	}
}

func TestLadybugDriverSeedsRevisionMigrationForLegacyState(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul-legacy-revisions.lbug")
	createdAt := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	store, err := newLadybugStore(Config{
		Driver:          StorageDriverLadybug,
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	legacy := emptyPersistedState()
	legacy.Sequence = 10
	legacy.Sources["src-legacy"] = Source{ID: "src-legacy", SpaceID: "default", Kind: "note", CreatedAt: createdAt}
	legacy.Episodes["ep-legacy"] = Episode{ID: "ep-legacy", SpaceID: "default", Kind: "note", Content: "legacy", SourceID: "src-legacy", IngestedAt: createdAt}
	legacy.Entities["entity:legacy"] = Entity{ID: "entity:legacy", SpaceID: "default", Type: "Project", CanonicalName: "Legacy", CreatedAt: createdAt, UpdatedAt: createdAt}
	legacy.Facts["fact-legacy"] = Fact{
		ID:                   "fact-legacy",
		SpaceID:              "default",
		Predicate:            "HAS_STATE",
		SubjectID:            "entity:legacy",
		ValueText:            "legacy state",
		Status:               factStatusActive,
		CreatedAt:            createdAt,
		UpdatedAt:            createdAt,
		SupportingEpisodeIDs: []string{"ep-legacy"},
	}
	if err := store.Save(legacy); err != nil {
		t.Fatalf("save legacy projection: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}

	eng, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("open migrated engine: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close migrated engine: %v", err)
	}

	reopened, err := Open(ctx, Config{
		Driver:       StorageDriverLadybug,
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("reopen migrated engine: %v", err)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}

	raw, err := lstore.Open(dbPath, true)
	if err != nil {
		t.Fatalf("open raw migrated store: %v", err)
	}
	defer raw.Close()

	assertRowCount(t, raw, "MATCH (m:YeoulMigration {id: 'bitemporal_revision_seed_v1'}) RETURN m.id", 1)
	assertRowCount(t, raw, "MATCH (r:EntityRevision {id: 'seed:entity:legacy'}) RETURN r.id", 1)
	assertRowCount(t, raw, "MATCH (r:FactRevision {id: 'seed:fact-legacy:current'}) RETURN r.id", 1)
}

func TestLadybugDriverPreservesMultipleSupersedesOnReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.lbug")
	eng, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open ladybug engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "multi supersede", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "multi supersede"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	oldA, err := eng.AssertFact(ctx, FactInput{ID: "fact:old-a", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old a", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert old a: %v", err)
	}
	oldB, err := eng.AssertFact(ctx, FactInput{ID: "fact:old-b", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old b", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert old b: %v", err)
	}
	newFact, err := eng.AssertFact(ctx, FactInput{ID: "fact:new", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "new", Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert new: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	reopened, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("reopen ladybug engine: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()
	got, err := reopened.GetFact(ctx, newFact.ID)
	if err != nil {
		t.Fatalf("get new fact: %v", err)
	}
	ids := metadataStringIDs(got.Metadata["supersedes"])
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[oldA.ID] || !seen[oldB.ID] {
		t.Fatalf("expected all superseded facts after reopen, got %#v", got.Metadata)
	}
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
