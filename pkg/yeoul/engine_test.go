package yeoul

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEngineRoundTripInMemory(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "chat_message",
		Content: "We decided to keep raw Cypher internal.",
		Source: SourceInput{
			Kind:        "chat",
			ExternalRef: "thread-001",
		},
		GroupID: "project:yeoul",
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	project, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		Namespace:     "default",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}

	storage, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Database",
		Namespace:     "default",
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
		ValueText:            "Yeoul uses Ladybug as its storage engine.",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		ObservedAt:           time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	gotFact, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if gotFact.Predicate != "USES_STORAGE_ENGINE" {
		t.Fatalf("unexpected predicate: %s", gotFact.Predicate)
	}

	search, err := eng.Search(ctx, SearchRequest{
		Meta: QueryMeta{
			SpaceID: "default",
		},
		QueryText: "Ladybug",
		Include: Include{
			Provenance:         true,
			SupportingEpisodes: true,
		},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("expected at least one search hit")
	}

	prov, err := eng.Provenance(ctx, ProvenanceRequest{
		Meta: QueryMeta{
			SpaceID: "default",
		},
		Kind: "fact",
		ID:   fact.ID,
	})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if len(prov.Nodes) < 2 {
		t.Fatalf("expected provenance graph nodes, got %d", len(prov.Nodes))
	}
}

func TestLifecycleTransitions(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "Ownership moved from A to B.",
		Source: SourceInput{
			Kind: "note",
		},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	task, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Task",
		CanonicalName: "api-spec-finalization",
	})
	if err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	ownerA, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Person",
		CanonicalName: "A",
	})
	if err != nil {
		t.Fatalf("upsert owner A: %v", err)
	}
	ownerB, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Person",
		CanonicalName: "B",
	})
	if err != nil {
		t.Fatalf("upsert owner B: %v", err)
	}

	oldFact, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerA.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}

	transition, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerB.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		ValidFrom:            time.Now().UTC(),
	}, "owner changed")
	if err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	if transition.OldFactID != oldFact.ID {
		t.Fatalf("unexpected old fact id: %s", transition.OldFactID)
	}

	storedOldFact, err := eng.GetFact(ctx, oldFact.ID)
	if err != nil {
		t.Fatalf("get old fact: %v", err)
	}
	if storedOldFact.Status != factStatusSuperseded {
		t.Fatalf("expected superseded status, got %s", storedOldFact.Status)
	}

	retract, err := eng.RetractFact(ctx, transition.NewFactID, "manual correction")
	if err != nil {
		t.Fatalf("retract new fact: %v", err)
	}
	if retract.Status != factStatusRetracted {
		t.Fatalf("expected retracted status, got %s", retract.Status)
	}
}

func TestHistoricalFactReadBeforeSupersedeAndRetract(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	current := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	rawEng.now = func() time.Time {
		current = current.Add(time.Microsecond)
		return current
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-hist",
		SpaceID: "default",
		Kind:    "note",
		Content: "status changed",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	project, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "project:hist",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Hist",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	oldFact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-old",
		SpaceID:              "default",
		Predicate:            "HAS_STATUS",
		SubjectID:            project.ID,
		ValueText:            "alpha",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}
	beforeTransition := oldFact.CreatedAt.Add(time.Nanosecond)
	transition, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID:                   "fact-new",
		SpaceID:              "default",
		Predicate:            "HAS_STATUS",
		SubjectID:            project.ID,
		ValueText:            "beta",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "status update")
	if err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	supersededFact, err := eng.GetFact(ctx, oldFact.ID)
	if err != nil {
		t.Fatalf("get superseded fact: %v", err)
	}
	afterSupersedeBeforeRetract := supersededFact.UpdatedAt.Add(time.Nanosecond)
	if _, err := eng.RetractFact(ctx, transition.NewFactID, "bad source"); err != nil {
		t.Fatalf("retract fact: %v", err)
	}

	historical, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
		Temporal:   TemporalFilter{AsOf: &beforeTransition},
	})
	if err != nil {
		t.Fatalf("historical lookup before supersede: %v", err)
	}
	if len(historical.Facts) != 1 || historical.Facts[0].ID != oldFact.ID {
		t.Fatalf("expected old fact before supersede, got %#v", historical.Facts)
	}

	mid, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
		Temporal:   TemporalFilter{AsOf: &afterSupersedeBeforeRetract},
	})
	if err != nil {
		t.Fatalf("historical lookup after supersede: %v", err)
	}
	if len(mid.Facts) != 1 || mid.Facts[0].ID != transition.NewFactID {
		t.Fatalf("expected new fact after supersede, got %#v", mid.Facts)
	}
}

func TestEntityVersionAtUsesHistorySnapshots(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "entity:history",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Original",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	beforeUpdate := time.Now().UTC()
	time.Sleep(time.Millisecond)
	if _, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            entity.ID,
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Renamed",
	}); err != nil {
		t.Fatalf("update entity: %v", err)
	}
	record, err := eng.GetRecord(ctx, GetRecordRequest{
		Kind:     "entity",
		ID:       entity.ID,
		Temporal: TemporalFilter{AsOf: &beforeUpdate},
	})
	if err != nil {
		t.Fatalf("get historical entity: %v", err)
	}
	historical, ok := record.Record.(*Entity)
	if !ok {
		t.Fatalf("expected entity record, got %T", record.Record)
	}
	if historical.CanonicalName != "Original" {
		t.Fatalf("expected historical canonical name, got %q", historical.CanonicalName)
	}
}

func TestProvenanceIncludesSupersedeChain(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	episode, _ := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-prov",
		SpaceID: "default",
		Kind:    "note",
		Content: "ownership changed",
		Source:  SourceInput{Kind: "note"},
	})
	task, _ := eng.UpsertEntity(ctx, EntityInput{ID: "task:prov", SpaceID: "default", Type: "Task", CanonicalName: "Prov"})
	a, _ := eng.UpsertEntity(ctx, EntityInput{ID: "person:a", SpaceID: "default", Type: "Person", CanonicalName: "A"})
	b, _ := eng.UpsertEntity(ctx, EntityInput{ID: "person:b", SpaceID: "default", Type: "Person", CanonicalName: "B"})
	oldFact, _ := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-prov-old",
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             a.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	transition, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID:                   "fact-prov-new",
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             b.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "owner update")
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}

	prov, err := eng.Provenance(ctx, ProvenanceRequest{
		Meta:     QueryMeta{SpaceID: "default"},
		Kind:     "fact",
		ID:       oldFact.ID,
		Temporal: TemporalFilter{IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	foundEdge := false
	foundNode := false
	for _, edge := range prov.Edges {
		if edge.Type == "SUPERSEDES" && edge.FromID == transition.NewFactID && edge.ToID == oldFact.ID {
			foundEdge = true
		}
	}
	for _, node := range prov.Nodes {
		if node.ID == transition.NewFactID {
			foundNode = true
		}
	}
	if !foundEdge || !foundNode {
		t.Fatalf("expected supersede chain in provenance, got nodes=%#v edges=%#v", prov.Nodes, prov.Edges)
	}
}

func TestSearchRespectsSpaceIsolation(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	epDefault, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "default memory",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest default episode: %v", err)
	}
	epOther, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "other",
		Kind:    "note",
		Content: "other memory",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest other episode: %v", err)
	}

	defaultEntity, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("default entity: %v", err)
	}
	otherEntity, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "other",
		Type:          "Project",
		CanonicalName: "Yeoul Other",
	})
	if err != nil {
		t.Fatalf("other entity: %v", err)
	}

	if _, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "HAS_LABEL",
		SubjectID:            defaultEntity.ID,
		ValueText:            "default-only",
		SupportingEpisodeIDs: []string{epDefault.EpisodeID},
	}); err != nil {
		t.Fatalf("assert default fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "other",
		Predicate:            "HAS_LABEL",
		SubjectID:            otherEntity.ID,
		ValueText:            "other-only",
		SupportingEpisodeIDs: []string{epOther.EpisodeID},
	}); err != nil {
		t.Fatalf("assert other fact: %v", err)
	}

	defaultSearch, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "only",
	})
	if err != nil {
		t.Fatalf("default search: %v", err)
	}
	if len(defaultSearch.Hits) != 1 || defaultSearch.Hits[0].RecordID == "" {
		t.Fatalf("unexpected default hits: %#v", defaultSearch.Hits)
	}

	otherSearch, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "other"},
		QueryText: "only",
	})
	if err != nil {
		t.Fatalf("other search: %v", err)
	}
	if len(otherSearch.Hits) != 1 || otherSearch.Hits[0].RecordID == defaultSearch.Hits[0].RecordID {
		t.Fatalf("unexpected other hits: %#v", otherSearch.Hits)
	}
}

func TestQueryFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	ep1, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:         "ep-1",
		SpaceID:    "default",
		Kind:       "note",
		Content:    "Yeoul uses Ladybug.",
		ObservedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Source:     SourceInput{Kind: "note", ExternalRef: "thread-1"},
	})
	if err != nil {
		t.Fatalf("ingest ep1: %v", err)
	}
	ep2, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:         "ep-2",
		SpaceID:    "default",
		Kind:       "note",
		Content:    "Yeoul also stores benchmarks.",
		ObservedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		Source:     SourceInput{Kind: "note", ExternalRef: "thread-2"},
	})
	if err != nil {
		t.Fatalf("ingest ep2: %v", err)
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
	ladybug, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "database:ladybug",
		SpaceID:       "default",
		Type:          "Database",
		CanonicalName: "Ladybug",
	})
	if err != nil {
		t.Fatalf("upsert ladybug: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-1",
		SpaceID:              "default",
		Predicate:            "USES_STORAGE_ENGINE",
		SubjectID:            project.ID,
		ObjectID:             ladybug.ID,
		ValueText:            "Yeoul uses Ladybug.",
		SupportingEpisodeIDs: []string{ep1.EpisodeID},
		ObservedAt:           time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("assert fact-1: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-2",
		SpaceID:              "default",
		Predicate:            "TRACKS",
		SubjectID:            project.ID,
		ValueText:            "Yeoul tracks benchmarks.",
		SupportingEpisodeIDs: []string{ep2.EpisodeID},
		ObservedAt:           time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("assert fact-2: %v", err)
	}

	searchPage1, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "Yeoul",
		AnchorIDs: []string{project.ID},
		Page:      Page{Limit: 1},
	})
	if err != nil {
		t.Fatalf("search page1: %v", err)
	}
	if len(searchPage1.Hits) != 1 || searchPage1.Meta.NextCursor == "" {
		t.Fatalf("unexpected search page1: %#v", searchPage1)
	}
	searchPage2, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "Yeoul",
		AnchorIDs: []string{project.ID},
		Page:      Page{Limit: 10, Cursor: searchPage1.Meta.NextCursor},
	})
	if err != nil {
		t.Fatalf("search page2: %v", err)
	}
	if len(searchPage2.Hits) == 0 {
		t.Fatal("expected second search page")
	}

	factsPage1, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
		Page:       Page{Limit: 1},
	})
	if err != nil {
		t.Fatalf("lookup facts page1: %v", err)
	}
	if len(factsPage1.Facts) != 1 || factsPage1.Meta.NextCursor == "" {
		t.Fatalf("unexpected facts page1: %#v", factsPage1)
	}

	asOf := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	timeline, err := eng.Timeline(ctx, TimelineRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		AnchorIDs: []string{project.ID},
		Temporal:  TemporalFilter{AsOf: &asOf},
	})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	for _, event := range timeline.Events {
		if event.Timestamp.After(asOf) {
			t.Fatalf("timeline included future event: %#v", event)
		}
	}

	provenance, err := eng.Provenance(ctx, ProvenanceRequest{
		Meta:     QueryMeta{SpaceID: "default"},
		Kind:     "fact",
		ID:       "fact-1",
		MaxDepth: 1,
	})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	for _, node := range provenance.Nodes {
		if node.Type == "Source" {
			t.Fatalf("expected max-depth=1 to exclude source nodes: %#v", provenance.Nodes)
		}
	}

	hood, err := eng.Neighborhood(ctx, NeighborhoodRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		AnchorIDs: []string{project.ID},
		MaxHops:   2,
		NodeTypes: []string{"Entity", "Fact", "Episode"},
	})
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	if len(hood.Nodes) < 3 {
		t.Fatalf("expected expanded neighborhood, got %#v", hood)
	}
}

func TestGetRecordTemporalVisibility(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "entity:temporal",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Temporal",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	before := entity.CreatedAt.Add(-time.Second)
	_, err = eng.GetRecord(ctx, GetRecordRequest{
		Kind:     "entity",
		ID:       entity.ID,
		Temporal: TemporalFilter{AsOf: &before},
	})
	if err == nil {
		t.Fatal("expected get record to reject pre-creation as_of")
	}
	if !strings.Contains(err.Error(), "not visible") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIngestBatchIsAtomic(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	_, err = eng.IngestBatch(ctx, BatchInput{
		Episodes: []EpisodeInput{
			{
				ID:      "ep-batch",
				SpaceID: "default",
				Kind:    "note",
				Content: "batch atomicity",
				Source:  SourceInput{Kind: "note", ExternalRef: "batch"},
			},
		},
		Facts: []FactInput{
			{
				ID:                   "fact-invalid",
				SpaceID:              "default",
				Predicate:            "BROKEN",
				SubjectID:            "missing-entity",
				SupportingEpisodeIDs: []string{"ep-batch"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected batch failure")
	}

	search, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "batch atomicity",
	})
	if err != nil {
		t.Fatalf("search after failed batch: %v", err)
	}
	if len(search.Hits) != 0 {
		t.Fatalf("expected failed batch to leave no records, got %#v", search.Hits)
	}
}

func TestAssertFactRequiresSupportingEpisode(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	entity, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	_, err = eng.AssertFact(ctx, FactInput{
		SpaceID:   "default",
		Predicate: "HAS_STATE",
		SubjectID: entity.ID,
		ValueText: "no provenance",
	})
	if err == nil {
		t.Fatal("expected provenance validation error")
	}
}

func TestEpisodeConflictingReplayIsRejected(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	_, err = eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-fixed",
		SpaceID: "default",
		Kind:    "note",
		Content: "original",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("initial ingest: %v", err)
	}

	_, err = eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-fixed",
		SpaceID: "default",
		Kind:    "note",
		Content: "changed",
		Source:  SourceInput{Kind: "note"},
	})
	if err == nil {
		t.Fatal("expected conflicting replay error")
	}
}

type countingStore struct {
	state     persistedState
	saveCount int
}

func (s *countingStore) Load() (*persistedState, error) {
	state := s.state
	if state.Version == 0 {
		state = emptyPersistedState()
	}
	state.Sources = defaultSources(state.Sources)
	state.Episodes = defaultEpisodes(state.Episodes)
	state.Entities = defaultEntities(state.Entities)
	state.Facts = defaultFacts(state.Facts)
	return &state, nil
}

func (s *countingStore) Save(state persistedState) error {
	s.saveCount++
	s.state = state
	return nil
}

func (s *countingStore) Close() error {
	return nil
}

func TestSupersedeFactPersistsOnce(t *testing.T) {
	ctx := context.Background()
	store := &countingStore{}
	eng := newEngine(Config{}, store)

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "note",
		Content: "Ownership moved from A to B.",
		Source:  SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	task, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Task",
		CanonicalName: "api-spec-finalization",
	})
	if err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	ownerA, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Person",
		CanonicalName: "A",
	})
	if err != nil {
		t.Fatalf("upsert owner A: %v", err)
	}
	ownerB, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Person",
		CanonicalName: "B",
	})
	if err != nil {
		t.Fatalf("upsert owner B: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerA.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	before := store.saveCount
	if _, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		SpaceID:              "default",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerB.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		ValidFrom:            time.Now().UTC(),
	}, "owner changed"); err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	if got := store.saveCount - before; got != 1 {
		t.Fatalf("expected exactly one durable save during supersede, got %d", got)
	}
}
