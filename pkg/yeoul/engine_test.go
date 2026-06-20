package yeoul

import (
	"context"
	"errors"
	"slices"
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

func TestSearchExpandsFromEntityHitToAdjacentFact(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "ownership note", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "project:graph-aware", Type: "Project", CanonicalName: "GraphAwareNeedle"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	team, err := eng.UpsertEntity(ctx, EntityInput{ID: "team:alpha", Type: "Team", CanonicalName: "Team Alpha"})
	if err != nil {
		t.Fatalf("upsert team: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:graph-aware",
		Predicate:            "HAS_OWNER",
		SubjectID:            entity.ID,
		ObjectID:             team.ID,
		ValueText:            "owned by team alpha",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	secondHop, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:graph-aware-hop2",
		Predicate:            "HAS_CHANNEL",
		SubjectID:            team.ID,
		ValueText:            "slack channel",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert second-hop fact: %v", err)
	}

	resp, err := eng.Search(ctx, SearchRequest{QueryText: "GraphAwareNeedle"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !searchHitHasReason(resp.Hits, fact.ID, "graph_expansion") {
		t.Fatalf("expected adjacent fact graph expansion hit, got %#v", resp.Hits)
	}
	if !searchHitHasReason(resp.Hits, secondHop.ID, "graph_expansion_bfs") {
		t.Fatalf("expected second-hop fact graph expansion hit, got %#v", resp.Hits)
	}
}

func TestSparseTermScoreUsesFrequencyAndLength(t *testing.T) {
	short := sparseTermScore("alpha", "alpha", nil)
	repeated := sparseTermScore("alpha", "alpha alpha", nil)
	long := sparseTermScore("alpha", "alpha beta gamma delta epsilon zeta eta theta", nil)
	if !(repeated > short && short > long) {
		t.Fatalf("expected sparse score frequency/length behavior, got repeated=%f short=%f long=%f", repeated, short, long)
	}
	stats := &sparseCorpusStats{DocCount: 10, DF: map[string]int{"common": 9, "rare": 1}}
	common := sparseTermScore("common", "common", stats)
	rare := sparseTermScore("rare", "rare", stats)
	if rare <= common {
		t.Fatalf("expected IDF to favor rare term, got rare=%f common=%f", rare, common)
	}
}

func TestSemanticSearchUsesVectorSimilarityFallback(t *testing.T) {
	matched, _, _ := matchSearch(SearchModeKeyword, "observabilty", "observability signal")
	if matched {
		t.Fatal("expected keyword search not to match typo")
	}
	matched, _, reason := matchSearch(SearchModeSemantic, "observabilty", "observability signal")
	if !matched || reason != "semantic_vector_similarity" {
		t.Fatalf("expected semantic vector similarity match, got matched=%v reason=%q", matched, reason)
	}
	matched, _, reason = matchSearch(SearchModeHybrid, "observabilty", "observability signal")
	if !matched || reason != "hybrid_vector_similarity" {
		t.Fatalf("expected hybrid vector similarity match, got matched=%v reason=%q", matched, reason)
	}
}

func TestDefaultSearchUsesHybridVectorSimilarity(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "observability signal", Source: SourceInput{Kind: "note"}}); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	resp, err := eng.Search(ctx, SearchRequest{QueryText: "observabilty", Types: []string{"episode"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Hits) != 1 || !slices.Contains(resp.Hits[0].Reasons, "hybrid_vector_similarity") {
		t.Fatalf("expected default hybrid vector hit, got %#v", resp.Hits)
	}
}

func searchHitHasReason(hits []SearchHit, recordID, reason string) bool {
	for _, hit := range hits {
		if hit.RecordID != recordID {
			continue
		}
		return slices.Contains(hit.Reasons, reason)
	}
	return false
}

func TestAssertFactRejectsLifecycleBypass(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "lifecycle bypass", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "target"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	_, err = eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_STATUS",
		SubjectID:            entity.ID,
		Status:               factStatusRetracted,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle-managed") {
		t.Fatalf("expected lifecycle status rejection, got %v", err)
	}

	_, err = eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_METADATA",
		SubjectID:            entity.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Metadata:             map[string]any{"superseded_by": "fact:later"},
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle-managed") {
		t.Fatalf("expected lifecycle metadata rejection, got %v", err)
	}

	_, err = eng.IngestBatch(ctx, BatchInput{Facts: []FactInput{{
		Predicate:            "HAS_BATCH_STATUS",
		SubjectID:            entity.ID,
		Status:               factStatusRetracted,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}}})
	if err == nil || !strings.Contains(err.Error(), "lifecycle-managed") {
		t.Fatalf("expected batch lifecycle status rejection, got %v", err)
	}
}

func TestSupersedeFactRejectsUnrelatedReplacement(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "owner changed", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	task, _ := eng.UpsertEntity(ctx, EntityInput{ID: "task:supersede", Type: "Task", CanonicalName: "Supersede"})
	owner, _ := eng.UpsertEntity(ctx, EntityInput{ID: "person:supersede", Type: "Person", CanonicalName: "Owner"})
	oldFact, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             owner.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	_, err = eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		Predicate:            "HAS_STATUS",
		SubjectID:            task.ID,
		ValueText:            "unrelated",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "bad replacement")
	if err == nil || !strings.Contains(err.Error(), "subject and predicate") {
		t.Fatalf("expected unrelated replacement rejection, got %v", err)
	}
}

func TestAssertFactCardinalityOneAutoSupersedesSlot(t *testing.T) {
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

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "slot changed", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	task, err := eng.UpsertEntity(ctx, EntityInput{ID: "task:auto-slot", Type: "Task", CanonicalName: "Auto Slot"})
	if err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	ownerA, err := eng.UpsertEntity(ctx, EntityInput{ID: "person:auto-a", Type: "Person", CanonicalName: "A"})
	if err != nil {
		t.Fatalf("upsert owner A: %v", err)
	}
	ownerB, err := eng.UpsertEntity(ctx, EntityInput{ID: "person:auto-b", Type: "Person", CanonicalName: "B"})
	if err != nil {
		t.Fatalf("upsert owner B: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-auto-old",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerA.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Cardinality:          "one",
	})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}
	beforeReplacement := oldFact.CreatedAt.Add(time.Nanosecond)
	newFact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-auto-new",
		Predicate:            "OWNED_BY",
		SubjectID:            task.ID,
		ObjectID:             ownerB.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Cardinality:          "one",
	})
	if err != nil {
		t.Fatalf("assert replacement fact: %v", err)
	}
	oldStored, err := eng.GetFact(ctx, oldFact.ID)
	if err != nil {
		t.Fatalf("get old fact: %v", err)
	}
	if oldStored.Status != factStatusSuperseded {
		t.Fatalf("expected old fact superseded, got %q", oldStored.Status)
	}
	if supersededBy, _ := oldStored.Metadata["superseded_by"].(string); supersededBy != newFact.ID {
		t.Fatalf("expected superseded_by %q, got %#v", newFact.ID, oldStored.Metadata)
	}

	historical, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{task.ID},
		Temporal:   TemporalFilter{AsOf: &beforeReplacement},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Facts) != 1 || historical.Facts[0].ID != oldFact.ID {
		t.Fatalf("expected old fact historically, got %#v", historical.Facts)
	}
	currentFacts, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{task.ID},
	})
	if err != nil {
		t.Fatalf("current lookup: %v", err)
	}
	if len(currentFacts.Facts) != 1 || currentFacts.Facts[0].ID != newFact.ID {
		t.Fatalf("expected new fact currently, got %#v", currentFacts.Facts)
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
	midWithInactive, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		SubjectIDs: []string{project.ID},
		Temporal:   TemporalFilter{AsOf: &afterSupersedeBeforeRetract, IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("historical inactive lookup after supersede: %v", err)
	}
	foundOld := false
	foundNew := false
	for _, fact := range midWithInactive.Facts {
		foundOld = foundOld || fact.ID == oldFact.ID
		foundNew = foundNew || fact.ID == transition.NewFactID
	}
	if len(midWithInactive.Facts) != 2 || !foundOld || !foundNew {
		t.Fatalf("expected include_inactive as_of to keep old and new facts, got %#v", midWithInactive.Facts)
	}
	graph, err := eng.Neighborhood(ctx, NeighborhoodRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		AnchorIDs: []string{project.ID},
		MaxHops:   1,
		Temporal:  TemporalFilter{AsOf: &beforeTransition},
	})
	if err != nil {
		t.Fatalf("historical neighborhood: %v", err)
	}
	if !graphHasNode(graph.Nodes, oldFact.ID) || graphHasNode(graph.Nodes, transition.NewFactID) {
		t.Fatalf("expected historical neighborhood to contain old fact only, got %#v", graph.Nodes)
	}
	prov, err := eng.Provenance(ctx, ProvenanceRequest{
		Meta:     QueryMeta{SpaceID: "default"},
		Kind:     "entity",
		ID:       project.ID,
		Temporal: TemporalFilter{AsOf: &beforeTransition},
	})
	if err != nil {
		t.Fatalf("historical provenance: %v", err)
	}
	if !provenanceHasNode(prov.Nodes, oldFact.ID) || provenanceHasNode(prov.Nodes, transition.NewFactID) {
		t.Fatalf("expected historical provenance to contain old fact only, got %#v", prov.Nodes)
	}
	timeline, err := eng.Timeline(ctx, TimelineRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		AnchorIDs: []string{project.ID},
		Temporal:  TemporalFilter{AsOf: &afterSupersedeBeforeRetract},
	})
	if err != nil {
		t.Fatalf("historical timeline: %v", err)
	}
	if !timelineHasEvent(timeline.Events, "evt:"+transition.NewFactID+":created") || timelineHasEvent(timeline.Events, "evt:"+transition.NewFactID+":retracted") {
		t.Fatalf("expected timeline to include new creation but not future retraction, got %#v", timeline.Events)
	}
	rawEng.mu.RLock()
	defer rawEng.mu.RUnlock()
	if len(rawEng.factRevisions) < 4 {
		t.Fatalf("expected append-only fact revisions, got %d", len(rawEng.factRevisions))
	}
}

func graphHasNode(nodes []GraphNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func provenanceHasNode(nodes []ProvenanceNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func timelineHasEvent(events []TimelineEvent, id string) bool {
	for _, event := range events {
		if event.EventID == id {
			return true
		}
	}
	return false
}

func TestTemporalFilterSeparatesKnowledgeAndDomainValidity(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	current := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	rawEng.now = func() time.Time {
		current = current.Add(time.Second)
		return current
	}

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:         "ep-bitemporal",
		Kind:       "note",
		Content:    "learned in June",
		ObservedAt: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		Source:     SourceInput{Kind: "note"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "thing:bitemporal", Type: "Thing", CanonicalName: "Bitemporal"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-bitemporal",
		Predicate:            "HAS_DOMAIN_VALIDITY",
		SubjectID:            entity.ID,
		ValueText:            "domain valid in February",
		ValidFrom:            time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:              time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		ObservedAt:           time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	beforeKnowledge := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	search, err := eng.Search(ctx, SearchRequest{
		QueryText: "February",
		Types:     []string{"fact"},
		Temporal:  TemporalFilter{AsOf: &beforeKnowledge},
	})
	if err != nil {
		t.Fatalf("knowledge search: %v", err)
	}
	if len(search.Hits) != 0 {
		t.Fatalf("expected knowledge_as_of before ingest to hide fact, got %#v", search.Hits)
	}

	validAt := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	search, err = eng.Search(ctx, SearchRequest{
		QueryText: "February",
		Types:     []string{"fact"},
		Temporal:  TemporalFilter{ValidAt: &validAt},
	})
	if err != nil {
		t.Fatalf("valid-at search: %v", err)
	}
	if len(search.Hits) != 1 || search.Hits[0].RecordID != "fact-bitemporal" {
		t.Fatalf("expected valid_at to match fact domain time, got %#v", search.Hits)
	}

	april := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	search, err = eng.Search(ctx, SearchRequest{
		QueryText: "February",
		Types:     []string{"fact"},
		Temporal:  TemporalFilter{ValidAt: &april},
	})
	if err != nil {
		t.Fatalf("valid-at after interval search: %v", err)
	}
	if len(search.Hits) != 0 {
		t.Fatalf("expected valid_at after interval to hide fact, got %#v", search.Hits)
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
	rawEng := eng.(*engine)
	rawEng.mu.RLock()
	defer rawEng.mu.RUnlock()
	if len(rawEng.entityRevisions) < 2 {
		t.Fatalf("expected append-only entity revisions, got %d", len(rawEng.entityRevisions))
	}
}

func TestUpsertEntityStableKeySurvivesRename(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	first, err := eng.UpsertEntity(ctx, EntityInput{
		Namespace:     "default",
		Type:          "Project",
		CanonicalName: "Old Name",
		StableKey:     "project-123",
	})
	if err != nil {
		t.Fatalf("upsert first entity: %v", err)
	}
	beforeRename := time.Now().UTC()
	time.Sleep(time.Millisecond)
	second, err := eng.UpsertEntity(ctx, EntityInput{
		Namespace:     "default",
		Type:          "Project",
		CanonicalName: "New Name",
		StableKey:     "project-123",
	})
	if err != nil {
		t.Fatalf("upsert renamed entity: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected stable key to keep entity id %q, got %q", first.ID, second.ID)
	}
	if second.CanonicalName != "New Name" {
		t.Fatalf("expected renamed canonical name, got %q", second.CanonicalName)
	}
	record, err := eng.GetRecord(ctx, GetRecordRequest{
		Kind:     "entity",
		ID:       first.ID,
		Temporal: TemporalFilter{AsOf: &beforeRename},
	})
	if err != nil {
		t.Fatalf("get historical entity: %v", err)
	}
	historical := record.Record.(*Entity)
	if historical.CanonicalName != "Old Name" {
		t.Fatalf("expected historical name, got %q", historical.CanonicalName)
	}
	if got, _ := second.Metadata["stable_key"].(string); got != "project-123" {
		t.Fatalf("expected stable key metadata, got %#v", second.Metadata)
	}
}

func TestUpsertEntityRejectsIdentityFieldChange(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:immutable", Namespace: "default", Type: "Project", CanonicalName: "Immutable"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	_, err = eng.UpsertEntity(ctx, EntityInput{ID: entity.ID, Namespace: "default", Type: "Person", CanonicalName: "Immutable"})
	if err == nil || !strings.Contains(err.Error(), "type is immutable") {
		t.Fatalf("expected immutable type rejection, got %v", err)
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
		Source:     SourceInput{Kind: "bench", ExternalRef: "thread-2"},
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

	sourceScopedSearch, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "Yeoul",
		Types:     []string{"fact"},
		Scope:     ScopeFilter{SourceIDs: []string{ep1.SourceID}, SourceKinds: []string{"note"}},
	})
	if err != nil {
		t.Fatalf("source scoped search: %v", err)
	}
	if len(sourceScopedSearch.Hits) != 1 || sourceScopedSearch.Hits[0].RecordID != "fact-1" {
		t.Fatalf("expected source scope to keep only fact-1, got %#v", sourceScopedSearch.Hits)
	}
	sourceScopedIncluded, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "Yeoul",
		Types:     []string{"fact"},
		Scope:     ScopeFilter{SourceKinds: []string{"note"}},
		Include:   Include{SupportingEpisodes: true},
	})
	if err != nil {
		t.Fatalf("source scoped included search: %v", err)
	}
	if len(sourceScopedIncluded.Included.Episodes) != 1 || sourceScopedIncluded.Included.Episodes[0].ID != ep1.EpisodeID {
		t.Fatalf("expected included support to respect source scope, got %#v", sourceScopedIncluded.Included.Episodes)
	}
	entityTypeScopedSearch, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		QueryText: "Yeoul",
		Types:     []string{"fact"},
		Scope:     ScopeFilter{EntityTypes: []string{"Database"}},
	})
	if err != nil {
		t.Fatalf("entity type scoped search: %v", err)
	}
	if len(entityTypeScopedSearch.Hits) != 1 || entityTypeScopedSearch.Hits[0].RecordID != "fact-1" {
		t.Fatalf("expected entity type scope to keep only fact-1, got %#v", entityTypeScopedSearch.Hits)
	}

	sourceScopedFacts, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:  QueryMeta{SpaceID: "default"},
		Scope: ScopeFilter{SourceKinds: []string{"note"}},
	})
	if err != nil {
		t.Fatalf("source scoped lookup: %v", err)
	}
	if len(sourceScopedFacts.Facts) != 1 || sourceScopedFacts.Facts[0].ID != "fact-1" {
		t.Fatalf("expected source scope to keep only fact-1, got %#v", sourceScopedFacts.Facts)
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
	sourceScopedTimeline, err := eng.Timeline(ctx, TimelineRequest{
		Meta:       QueryMeta{SpaceID: "default"},
		AnchorIDs:  []string{project.ID},
		EventTypes: []string{"fact_created"},
		Scope:      ScopeFilter{SourceKinds: []string{"note"}},
	})
	if err != nil {
		t.Fatalf("source scoped timeline: %v", err)
	}
	if len(sourceScopedTimeline.Events) != 1 || sourceScopedTimeline.Events[0].RecordID != "fact-1" {
		t.Fatalf("expected source scope to keep only fact-1 timeline event, got %#v", sourceScopedTimeline.Events)
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
	sourceScopedHood, err := eng.Neighborhood(ctx, NeighborhoodRequest{
		Meta:      QueryMeta{SpaceID: "default"},
		AnchorIDs: []string{project.ID},
		MaxHops:   2,
		Scope:     ScopeFilter{SourceKinds: []string{"note"}},
	})
	if err != nil {
		t.Fatalf("source scoped neighborhood: %v", err)
	}
	for _, node := range sourceScopedHood.Nodes {
		if node.ID == "fact-2" {
			t.Fatalf("source scoped neighborhood included filtered fact: %#v", sourceScopedHood.Nodes)
		}
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

func TestIngestBatchRejectsRevisionImport(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	_, err = eng.IngestBatch(ctx, BatchInput{
		EntityRevisions: []EntityRevision{{ID: "entityrev:restore", EntityID: "entity:restore"}},
	})
	if err == nil {
		t.Fatal("expected public revision import to fail")
	}
}

func TestImportedRevisionIDsDoNotCollideWithGeneratedIDs(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	rawEng.mu.Lock()
	defer rawEng.mu.Unlock()
	rawEng.factRevisions["factrev_000001"] = FactRevision{ID: "factrev_000001", FactID: "fact:restore"}
	if got := rawEng.newIDLocked("factrev"); got == "factrev_000001" {
		t.Fatalf("generated colliding revision id: %s", got)
	}
	if _, ok := rawEng.factRevisions["factrev_000001"]; !ok {
		t.Fatal("expected imported revision to remain present")
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
	state.FactRevisions = defaultFactRevisions(state.FactRevisions)
	state.EntityRevisions = defaultEntityRevisions(state.EntityRevisions)
	state.MigrationWatermarks = defaultMigrationWatermarks(state.MigrationWatermarks)
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

type failingStore struct {
	countingStore
	failSave bool
}

func (s *failingStore) Save(state persistedState) error {
	if s.failSave {
		return errors.New("save failed")
	}
	return s.countingStore.Save(state)
}

func TestSupersedeFactRestoresMemoryOnSaveFailure(t *testing.T) {
	ctx := context.Background()
	store := &failingStore{}
	eng := newEngine(Config{}, store)

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "rollback", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "rollback"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	store.failSave = true
	_, err = eng.SupersedeFact(ctx, oldFact.ID, FactInput{ID: "fact-new", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "new", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "test")
	if err == nil {
		t.Fatal("expected save failure")
	}
	gotOld, err := eng.GetFact(ctx, oldFact.ID)
	if err != nil {
		t.Fatalf("get old fact: %v", err)
	}
	if gotOld.Status != factStatusActive {
		t.Fatalf("expected old fact restored active, got %s", gotOld.Status)
	}
	if _, err := eng.GetFact(ctx, "fact-new"); err == nil {
		t.Fatal("expected replacement fact to be rolled back")
	}
}

func TestMutatorsRestoreMemoryOnSaveFailure(t *testing.T) {
	ctx := context.Background()
	store := &failingStore{}
	eng := newEngine(Config{}, store)

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "seed", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "seed"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "seed", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	store.failSave = true
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "ep-failed", Kind: "note", Content: "failed", Source: SourceInput{Kind: "note"}}); err == nil {
		t.Fatal("expected ingest save failure")
	}
	if _, err := eng.GetEpisode(ctx, "ep-failed"); err == nil {
		t.Fatal("expected failed episode to be rolled back")
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:failed", Type: "Thing", CanonicalName: "failed"}); err == nil {
		t.Fatal("expected entity save failure")
	}
	if _, err := eng.GetEntity(ctx, "entity:failed"); err == nil {
		t.Fatal("expected failed entity to be rolled back")
	}
	if _, err := eng.AssertFact(ctx, FactInput{ID: "fact-failed", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "failed", SupportingEpisodeIDs: []string{episode.EpisodeID}}); err == nil {
		t.Fatal("expected fact save failure")
	}
	if _, err := eng.GetFact(ctx, "fact-failed"); err == nil {
		t.Fatal("expected failed fact to be rolled back")
	}
	if _, err := eng.RetractFact(ctx, fact.ID, "failed"); err == nil {
		t.Fatal("expected retract save failure")
	}
	gotFact, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if gotFact.Status != factStatusActive {
		t.Fatalf("expected retraction rollback, got %s", gotFact.Status)
	}
}

func TestSourceIDsAreSpaceScoped(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	first, err := eng.IngestEpisode(ctx, EpisodeInput{SpaceID: "a", Kind: "note", Content: "same", Source: SourceInput{Kind: "note", ExternalRef: "same", Metadata: map[string]any{"space": "a"}}})
	if err != nil {
		t.Fatalf("ingest first: %v", err)
	}
	second, err := eng.IngestEpisode(ctx, EpisodeInput{SpaceID: "b", Kind: "note", Content: "same", Source: SourceInput{Kind: "note", ExternalRef: "same", Metadata: map[string]any{"space": "b"}}})
	if err != nil {
		t.Fatalf("ingest second: %v", err)
	}
	if first.SourceID == second.SourceID {
		t.Fatalf("expected space-scoped source IDs, got %q", first.SourceID)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{SpaceID: "b", Kind: "note", Content: "bad", SourceID: first.SourceID, Source: SourceInput{Kind: "note"}}); err == nil {
		t.Fatal("expected explicit cross-space source reuse to fail")
	}
}

func TestCardinalityOneBackfillDoesNotInvalidateNonOverlappingCurrent(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "validity", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "validity"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	current, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "current", ValidFrom: june, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert current fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "backfill", ValidTo: may, Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}}); err != nil {
		t.Fatalf("assert bounded backfill: %v", err)
	}
	got, err := eng.GetFact(ctx, current.ID)
	if err != nil {
		t.Fatalf("get current fact: %v", err)
	}
	if got.Status != factStatusActive {
		t.Fatalf("expected current fact to remain active, got %s", got.Status)
	}
	june15 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	facts, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{entity.ID}, Temporal: TemporalFilter{ValidAt: &june15}})
	if err != nil {
		t.Fatalf("lookup current valid_at: %v", err)
	}
	if len(facts.Facts) != 1 || facts.Facts[0].ID != current.ID {
		t.Fatalf("expected current fact at june valid_at, got %#v", facts.Facts)
	}
}

func TestCardinalityOneCorrectionKeepsTransactionHistory(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rawEng.now = func() time.Time {
		now = now.Add(time.Microsecond)
		return now
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "correction", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "correction"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	validFrom := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	oldFact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old", ValidFrom: validFrom, Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}
	beforeCorrection := oldFact.CreatedAt.Add(time.Nanosecond)
	newFact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "new", ValidFrom: validFrom, Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert correction fact: %v", err)
	}
	historical, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{entity.ID}, Temporal: TemporalFilter{AsOf: &beforeCorrection, ValidAt: &validFrom}})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Facts) != 1 || historical.Facts[0].ID != oldFact.ID {
		t.Fatalf("expected old fact historically, got %#v", historical.Facts)
	}
	current, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{entity.ID}, Temporal: TemporalFilter{ValidAt: &validFrom}})
	if err != nil {
		t.Fatalf("current lookup: %v", err)
	}
	if len(current.Facts) != 1 || current.Facts[0].ID != newFact.ID {
		t.Fatalf("expected new fact currently, got %#v", current.Facts)
	}
}

func TestLegacyBitemporalSeedPreservesHistoricalActiveFact(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	asOfBeforeSupersede := createdAt.Add(time.Minute)

	rawEng.applyState(persistedState{
		Version: 1,
		Sources: map[string]Source{
			"src:legacy": {ID: "src:legacy", SpaceID: "default", Kind: "note", CreatedAt: createdAt},
		},
		Episodes: map[string]Episode{
			"ep:legacy": {ID: "ep:legacy", SpaceID: "default", Kind: "note", Content: "legacy", SourceID: "src:legacy", IngestedAt: createdAt},
		},
		Entities: map[string]Entity{
			"entity:legacy": {ID: "entity:legacy", SpaceID: "default", Type: "Thing", CanonicalName: "Legacy", CreatedAt: createdAt, UpdatedAt: createdAt},
		},
		Facts: map[string]Fact{
			"fact:legacy": {
				ID:                   "fact:legacy",
				SpaceID:              "default",
				Predicate:            "HAS_STATE",
				SubjectID:            "entity:legacy",
				ValueText:            "legacy state",
				Status:               factStatusSuperseded,
				CreatedAt:            createdAt,
				UpdatedAt:            updatedAt,
				SupportingEpisodeIDs: []string{"ep:legacy"},
				Metadata:             map[string]any{"superseded_by": "fact:new", "keep": "yes"},
			},
		},
	})

	historical, err := eng.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{"entity:legacy"},
		Temporal:   TemporalFilter{AsOf: &asOfBeforeSupersede},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Facts) != 1 || historical.Facts[0].ID != "fact:legacy" || historical.Facts[0].Status != factStatusActive {
		t.Fatalf("expected seeded active historical fact, got %#v", historical.Facts)
	}
	if _, ok := rawEng.factRevisions["seed:fact:legacy:initial"]; !ok {
		t.Fatalf("expected initial seed revision, got %#v", rawEng.factRevisions)
	}
	if _, ok := historical.Facts[0].Metadata["superseded_by"]; ok {
		t.Fatalf("expected lifecycle metadata stripped from historical seed, got %#v", historical.Facts[0].Metadata)
	}
}

func TestTimelineAsOfUsesEpisodeIngestKnowledgeTime(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	observedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ingestedAt := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	asOfBeforeIngest := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	rawEng.applyState(persistedState{
		Version: 1,
		Sources: map[string]Source{
			"src:late": {ID: "src:late", SpaceID: "default", Kind: "note", CreatedAt: ingestedAt},
		},
		Episodes: map[string]Episode{
			"ep:late": {ID: "ep:late", SpaceID: "default", Kind: "note", Content: "known later", SourceID: "src:late", ObservedAt: observedAt, IngestedAt: ingestedAt},
		},
	})

	timeline, err := eng.Timeline(ctx, TimelineRequest{Temporal: TemporalFilter{AsOf: &asOfBeforeIngest}})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if timelineHasEvent(timeline.Events, "evt:ep:late") {
		t.Fatalf("expected as_of before ingest to hide episode, got %#v", timeline.Events)
	}
}

func TestTimelineIncludesLifecycleEventsByDefaultAtTransactionTime(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	rawEng.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "backdated correction", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "timeline lifecycle"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert old: %v", err)
	}
	validFrom := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	_, err = eng.SupersedeFact(ctx, oldFact.ID, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "new", ValidFrom: validFrom, SupportingEpisodeIDs: []string{episode.EpisodeID}}, "backdated")
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	oldStored, err := eng.GetFact(ctx, oldFact.ID)
	if err != nil {
		t.Fatalf("get old: %v", err)
	}

	timeline, err := eng.Timeline(ctx, TimelineRequest{AnchorIDs: []string{entity.ID}})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	var superseded TimelineEvent
	for _, event := range timeline.Events {
		if event.EventID == "evt:"+oldFact.ID+":superseded" {
			superseded = event
			break
		}
	}
	if superseded.EventID == "" {
		t.Fatalf("expected default timeline to include superseded event, got %#v", timeline.Events)
	}
	if !superseded.Timestamp.Equal(oldStored.UpdatedAt) || superseded.Timestamp.Equal(validFrom) {
		t.Fatalf("expected superseded event at transaction time %s, got %s", oldStored.UpdatedAt, superseded.Timestamp)
	}
}

func TestCardinalityOneProvenanceIncludesAllAutoSupersededFacts(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "many replaced", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "multi replace"})
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
	if got := metadataStringIDs(newFact.Metadata["supersedes"]); !slices.Contains(got, oldA.ID) || !slices.Contains(got, oldB.ID) {
		t.Fatalf("expected replacement to record all superseded facts, got %#v", newFact.Metadata)
	}

	prov, err := eng.Provenance(ctx, ProvenanceRequest{
		Kind:     "fact",
		ID:       newFact.ID,
		Temporal: TemporalFilter{IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	edges := map[string]bool{}
	for _, edge := range prov.Edges {
		if edge.Type == "SUPERSEDES" && edge.FromID == newFact.ID {
			edges[edge.ToID] = true
		}
	}
	if !edges[oldA.ID] || !edges[oldB.ID] {
		t.Fatalf("expected provenance edges to both old facts, got %#v", prov.Edges)
	}
}

func TestFactSupportRejectsCrossSpaceEpisodeSource(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	now := time.Now().UTC()
	rawEng.sources["src:legacy"] = Source{ID: "src:legacy", SpaceID: "a", Kind: "note", ExternalRef: "legacy", CreatedAt: now}
	rawEng.episodes["ep:bad"] = Episode{ID: "ep:bad", SpaceID: "b", Kind: "note", Content: "bad support", SourceID: "src:legacy", IngestedAt: now}
	rawEng.entities["entity:bad"] = Entity{ID: "entity:bad", SpaceID: "b", Type: "Thing", CanonicalName: "bad", CreatedAt: now, UpdatedAt: now}
	rawEng.facts["fact:bad"] = Fact{ID: "fact:bad", SpaceID: "b", Predicate: "HAS_STATE", SubjectID: "entity:bad", ValueText: "bad support", Status: factStatusActive, CreatedAt: now, UpdatedAt: now, SupportingEpisodeIDs: []string{"ep:bad"}}

	result, err := eng.Search(ctx, SearchRequest{
		Meta:      QueryMeta{SpaceID: "b"},
		QueryText: "bad support",
		Types:     []string{"fact"},
		Include:   Include{Provenance: true, SupportingEpisodes: true},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("expected fact hit, got %#v", result.Hits)
	}
	if len(result.Included.Episodes) != 0 || len(result.Included.Sources) != 0 {
		t.Fatalf("expected malformed support excluded, got episodes=%#v sources=%#v", result.Included.Episodes, result.Included.Sources)
	}
}

func TestLegacySourceIDIsReusedWhenSpaceMatches(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	rawEng.sources["src:note:same"] = Source{ID: "src:note:same", SpaceID: "default", Kind: "note", ExternalRef: "same", CreatedAt: time.Now().UTC()}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "same", Source: SourceInput{Kind: "note", ExternalRef: "same"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if episode.SourceID != "src:note:same" {
		t.Fatalf("expected legacy source id reuse, got %q", episode.SourceID)
	}
}

func TestEpisodeSourceKindFilterMissingSourceFailsClosed(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	now := time.Now().UTC()
	episode := Episode{ID: "ep:missing-source", SpaceID: "default", Kind: "note", Content: "missing source", SourceID: "src:missing", IngestedAt: now}
	rawEng.episodes[episode.ID] = episode
	if RecordPassesSearchFilters(ctx, eng, &episode, SearchRequest{Scope: ScopeFilter{SourceKinds: []string{"note"}}}) {
		t.Fatal("expected missing source to fail closed")
	}
}

func TestNestedMetadataIsDeepCopied(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "metadata", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	metadata := map[string]any{"nested": map[string]any{"k": "v"}}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "metadata", Metadata: metadata})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{Predicate: "HAS_META", SubjectID: entity.ID, ValueText: "metadata", SupportingEpisodeIDs: []string{episode.EpisodeID}, Metadata: metadata})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	metadata["nested"].(map[string]any)["k"] = "mutated input"
	gotFact, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	gotFact.Metadata["nested"].(map[string]any)["k"] = "mutated output"
	gotFactAgain, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact again: %v", err)
	}
	if gotFactAgain.Metadata["nested"].(map[string]any)["k"] != "v" {
		t.Fatalf("expected fact metadata deep copy, got %#v", gotFactAgain.Metadata)
	}
	gotEntity, err := eng.GetEntity(ctx, entity.ID)
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if gotEntity.Metadata["nested"].(map[string]any)["k"] != "v" {
		t.Fatalf("expected entity metadata deep copy, got %#v", gotEntity.Metadata)
	}
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
