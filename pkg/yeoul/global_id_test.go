package yeoul

import (
	"context"
	"slices"
	"testing"
)

// requireInputInvalid asserts the error is a structured YEOUL_INPUT_INVALID
// and returns its details for conflict inspection.
func requireInputInvalid(t *testing.T, err error, context string) map[string]any {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s to be rejected, got nil error", context)
	}
	structured := unwrapYeoulError(err)
	if structured == nil {
		t.Fatalf("expected a structured error for %s, got %v", context, err)
	}
	if structured.Code != ErrInputInvalid {
		t.Fatalf("expected %s to fail with %s, got %s (%v)", context, ErrInputInvalid, structured.Code, err)
	}
	return structured.Details
}

func conflictKinds(t *testing.T, details map[string]any) []string {
	t.Helper()
	raw, ok := details["conflicting_kinds"]
	if !ok {
		t.Fatalf("expected conflicting_kinds in error details, got %#v", details)
	}
	kinds, ok := raw.([]string)
	if !ok {
		t.Fatalf("expected conflicting_kinds to be []string, got %T", raw)
	}
	return kinds
}

// seedGlobalIDFixtures creates an entity and a supported episode so fact
// assertions have the references they require.
func seedGlobalIDFixtures(t *testing.T, eng Engine, ctx context.Context) (entityID, episodeID string) {
	t.Helper()
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "id-fixture-entity", Type: "Thing", CanonicalName: "Fixture"})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "id-fixture-episode",
		Kind:    "note",
		Content: "fixture",
		Source:  SourceInput{Kind: "note", ExternalRef: "fixture"},
	})
	if err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	return entity.ID, episode.EpisodeID
}

func TestExplicitIDsAreGloballyUniqueAcrossKinds(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	entityID, episodeID := seedGlobalIDFixtures(t, eng, ctx)

	t.Run("episode_id_blocks_entity", func(t *testing.T) {
		if _, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: "shared-a", Kind: "note", Content: "a",
			Source: SourceInput{Kind: "note", ExternalRef: "a"},
		}); err != nil {
			t.Fatalf("ingest episode: %v", err)
		}
		_, err := eng.UpsertEntity(ctx, EntityInput{ID: "shared-a", Type: "Thing", CanonicalName: "A"})
		details := requireInputInvalid(t, err, "entity reusing an episode id")
		if !slices.Contains(conflictKinds(t, details), kindEpisode) {
			t.Fatalf("expected the episode kind in the conflict, got %#v", details)
		}
		if _, lookupErr := eng.GetEntity(ctx, "shared-a"); lookupErr == nil {
			t.Fatal("expected the rejected entity not to be written")
		}
	})

	t.Run("episode_id_blocks_fact", func(t *testing.T) {
		if _, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: "shared-b", Kind: "note", Content: "b",
			Source: SourceInput{Kind: "note", ExternalRef: "b"},
		}); err != nil {
			t.Fatalf("ingest episode: %v", err)
		}
		_, err := eng.AssertFact(ctx, FactInput{
			ID: "shared-b", Predicate: "HAS_SCOPE", SubjectID: entityID,
			SupportingEpisodeIDs: []string{episodeID},
		})
		details := requireInputInvalid(t, err, "fact reusing an episode id")
		if !slices.Contains(conflictKinds(t, details), kindEpisode) {
			t.Fatalf("expected the episode kind in the conflict, got %#v", details)
		}
		if _, lookupErr := eng.GetFact(ctx, "shared-b"); lookupErr == nil {
			t.Fatal("expected the rejected fact not to be written")
		}
	})

	t.Run("entity_id_blocks_episode", func(t *testing.T) {
		if _, err := eng.UpsertEntity(ctx, EntityInput{ID: "shared-c", Type: "Thing", CanonicalName: "C"}); err != nil {
			t.Fatalf("upsert entity: %v", err)
		}
		_, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: "shared-c", Kind: "note", Content: "c",
			Source: SourceInput{Kind: "note", ExternalRef: "c"},
		})
		details := requireInputInvalid(t, err, "episode reusing an entity id")
		if !slices.Contains(conflictKinds(t, details), kindEntity) {
			t.Fatalf("expected the entity kind in the conflict, got %#v", details)
		}
	})

	t.Run("entity_id_blocks_fact", func(t *testing.T) {
		if _, err := eng.UpsertEntity(ctx, EntityInput{ID: "shared-d", Type: "Thing", CanonicalName: "D"}); err != nil {
			t.Fatalf("upsert entity: %v", err)
		}
		_, err := eng.AssertFact(ctx, FactInput{
			ID: "shared-d", Predicate: "HAS_SCOPE", SubjectID: entityID,
			SupportingEpisodeIDs: []string{episodeID},
		})
		details := requireInputInvalid(t, err, "fact reusing an entity id")
		if !slices.Contains(conflictKinds(t, details), kindEntity) {
			t.Fatalf("expected the entity kind in the conflict, got %#v", details)
		}
	})

	t.Run("source_id_blocks_entity", func(t *testing.T) {
		if _, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: "ep-shared-source", Kind: "note", Content: "source",
			Source: SourceInput{ID: "shared-e", Kind: "note", ExternalRef: "e"},
		}); err != nil {
			t.Fatalf("ingest episode: %v", err)
		}
		_, err := eng.UpsertEntity(ctx, EntityInput{ID: "shared-e", Type: "Thing", CanonicalName: "E"})
		details := requireInputInvalid(t, err, "entity reusing a source id")
		if !slices.Contains(conflictKinds(t, details), kindSource) {
			t.Fatalf("expected the source kind in the conflict, got %#v", details)
		}
	})

	t.Run("fact_id_blocks_entity", func(t *testing.T) {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID: "shared-f", Predicate: "HAS_SCOPE", SubjectID: entityID,
			SupportingEpisodeIDs: []string{episodeID},
		}); err != nil {
			t.Fatalf("assert fact: %v", err)
		}
		_, err := eng.UpsertEntity(ctx, EntityInput{ID: "shared-f", Type: "Thing", CanonicalName: "F"})
		details := requireInputInvalid(t, err, "entity reusing a fact id")
		if !slices.Contains(conflictKinds(t, details), kindFact) {
			t.Fatalf("expected the fact kind in the conflict, got %#v", details)
		}
	})
}

// TestCrossKindConflictNamesEveryOwner covers the legacy-data case where one ID
// already sits in more than one kind's map: the error must name each kind so an
// operator can find the collision.
func TestCrossKindConflictNamesEveryOwner(t *testing.T) {
	rawEng, ctx := openIdentityTestEngine(t)
	rawEng.mu.Lock()
	rawEng.sources["multi"] = Source{ID: "multi", SpaceID: "default", Kind: "note", CreatedAt: rawEng.now()}
	rawEng.episodes["multi"] = Episode{ID: "multi", SpaceID: "default", Kind: "note", Content: "multi", IngestedAt: rawEng.now()}
	rawEng.mu.Unlock()

	_, err := rawEng.UpsertEntity(ctx, EntityInput{ID: "multi", Type: "Thing", CanonicalName: "Multi"})
	details := requireInputInvalid(t, err, "entity reusing an id owned by two kinds")
	kinds := conflictKinds(t, details)
	for _, want := range []string{kindSource, kindEpisode} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("expected %q in the conflict kinds, got %#v", want, kinds)
		}
	}
	if len(kinds) != 2 {
		t.Fatalf("expected exactly two conflicting kinds, got %#v", kinds)
	}
}

// TestSameKindExplicitIDReplayStillWorks guards the uniqueness change against
// over-rejection: an explicit ID reused by its own kind keeps the existing
// idempotent replay and upsert behavior.
func TestSameKindExplicitIDReplayStillWorks(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID: "replay-ep", Kind: "note", Content: "same",
		Source: SourceInput{Kind: "note", ExternalRef: "replay"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	replay, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID: "replay-ep", Kind: "note", Content: "same",
		Source: SourceInput{Kind: "note", ExternalRef: "replay"},
	})
	if err != nil {
		t.Fatalf("replay episode: %v", err)
	}
	if replay.Created {
		t.Fatal("expected an identical episode replay to report Created=false")
	}
	if replay.EpisodeID != episode.EpisodeID || replay.SourceID != episode.SourceID {
		t.Fatalf("expected the replay to reuse ids, got %#v", replay)
	}

	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "replay-entity", Type: "Thing", CanonicalName: "Replay"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	updated, err := eng.UpsertEntity(ctx, EntityInput{ID: "replay-entity", Type: "Thing", CanonicalName: "Replay Renamed"})
	if err != nil {
		t.Fatalf("re-upsert entity: %v", err)
	}
	if updated.ID != entity.ID || updated.CanonicalName != "Replay Renamed" {
		t.Fatalf("expected the same explicit id to stay updatable, got %#v", updated)
	}
}

// TestDerivedEntityIDRespectsGlobalUniqueness covers the order-dependent hole
// the explicit-ID checks left open: a derived entity ID is content-addressed,
// but an explicit episode, fact, or source ID can claim the same raw string
// first. The entity must be rejected instead of writing a state that its own
// Open-time global ID validation would refuse to load.
func TestDerivedEntityIDRespectsGlobalUniqueness(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	derived := EntityID("", "Thing", "Derived Collision")
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID: derived, Kind: "note", Content: "claims the derived entity id",
		Source: SourceInput{Kind: "note", ExternalRef: "derived-collision"},
	}); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	_, err = eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "Derived Collision"})
	details := requireInputInvalid(t, err, "derived entity reusing an episode id")
	if kinds := conflictKinds(t, details); !slices.Contains(kinds, kindEpisode) {
		t.Fatalf("expected the episode kind in the conflict, got %#v", kinds)
	}

	// The legacy-ID lookup must not open a second hole: the legacy spelling of
	// the same identity is also rejected when another kind owns it.
	legacy := legacyEntityID("", "Thing", "Derived Collision")
	if legacy != derived {
		if _, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: legacy, Kind: "note", Content: "claims the legacy entity id",
			Source: SourceInput{Kind: "note", ExternalRef: "legacy-collision"},
		}); err != nil {
			t.Fatalf("ingest legacy-claiming episode: %v", err)
		}
		_, err = eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "Derived Collision"})
		requireInputInvalid(t, err, "derived entity reusing a legacy episode id")
	}
}

// TestCompositeIDEncodingIsUnambiguous pins the length-prefixed encoding that
// replaced plain delimiter concatenation.
func TestCompositeIDEncodingIsUnambiguous(t *testing.T) {
	collisions := [][2][]string{
		{{"f", "e1:e2"}, {"f:e1", "e2"}},
		{{"a:b", "c"}, {"a", "b:c"}},
		{{"a", "b", "c"}, {"a:b", "c"}},
		{{"1:1", "x"}, {"1", "1:x"}},
	}
	for _, pair := range collisions {
		left := compositeID("edge", pair[0]...)
		right := compositeID("edge", pair[1]...)
		if left == right {
			t.Fatalf("composite ids aliased: %q for parts %#v and %#v", left, pair[0], pair[1])
		}
	}
	if got, want := compositeID("edge", "f", "e1:e2"), "edge:1:f:5:e1:e2"; got != want {
		t.Fatalf("unexpected composite id encoding: got %q want %q", got, want)
	}
}

// TestDelimiterCollisionEdgesDoNotAlias builds the exact pair that plain
// concatenation collapsed (fact "f" + episode "e1:e2" versus fact "f:e1" +
// episode "e2") and proves the neighborhood returns two distinct ASSERTS
// edges instead of one aliased edge.
func TestDelimiterCollisionEdgesDoNotAlias(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "ent-collision", Type: "Thing", CanonicalName: "Collision"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	for _, episodeID := range []string{"e1:e2", "e2"} {
		if _, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID: episodeID, Kind: "note", Content: "content " + episodeID,
			Source: SourceInput{Kind: "note", ExternalRef: "collision-" + episodeID},
		}); err != nil {
			t.Fatalf("ingest episode %q: %v", episodeID, err)
		}
	}
	for factID, episodeID := range map[string]string{"f": "e1:e2", "f:e1": "e2"} {
		if _, err := eng.AssertFact(ctx, FactInput{
			ID: factID, Predicate: "HAS_SCOPE", SubjectID: entity.ID,
			SupportingEpisodeIDs: []string{episodeID},
		}); err != nil {
			t.Fatalf("assert fact %q: %v", factID, err)
		}
	}

	neighborhood, err := eng.Neighborhood(ctx, NeighborhoodRequest{AnchorIDs: []string{"f", "f:e1"}, MaxHops: 1})
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	asserts := make(map[string]GraphEdge)
	for _, edge := range neighborhood.Edges {
		if edge.Type == "ASSERTS" {
			asserts[edge.ID] = edge
		}
	}
	if len(asserts) != 2 {
		t.Fatalf("expected two distinct ASSERTS edges, got %d: %#v", len(asserts), neighborhood.Edges)
	}
	if !slices.ContainsFunc(neighborhood.Edges, func(edge GraphEdge) bool {
		return edge.Type == "ASSERTS" && edge.FromID == "e1:e2" && edge.ToID == "f"
	}) {
		t.Fatalf("expected the e1:e2 -> f assertion edge, got %#v", neighborhood.Edges)
	}
	if !slices.ContainsFunc(neighborhood.Edges, func(edge GraphEdge) bool {
		return edge.Type == "ASSERTS" && edge.FromID == "e2" && edge.ToID == "f:e1"
	}) {
		t.Fatalf("expected the e2 -> f:e1 assertion edge, got %#v", neighborhood.Edges)
	}
}

// edgesOfType returns the edges of one type from a neighborhood response.
func edgesOfType(edges []GraphEdge, edgeType string) []GraphEdge {
	return slices.DeleteFunc(slices.Clone(edges), func(edge GraphEdge) bool {
		return edge.Type != edgeType
	})
}

// TestNeighborhoodNamespacesSubjectAndAssertsEdges covers the ambiguity that
// plain endpoint encoding left between SUBJECT/OBJECT and ASSERTS edges: an
// episode whose explicit id is the literal "subject" (or "object") must not
// share an id with the fact's SUBJECT (or OBJECT) edge.
func TestNeighborhoodNamespacesSubjectAndAssertsEdges(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	subject, err := eng.UpsertEntity(ctx, EntityInput{ID: "namespace-anchor", Type: "Thing", CanonicalName: "Anchor"})
	if err != nil {
		t.Fatalf("upsert subject entity: %v", err)
	}
	object, err := eng.UpsertEntity(ctx, EntityInput{ID: "namespace-object-entity", Type: "Thing", CanonicalName: "Object"})
	if err != nil {
		t.Fatalf("upsert object entity: %v", err)
	}

	tests := []struct {
		episodeID string
		objectID  string
	}{
		{episodeID: "subject"},
		{episodeID: "object", objectID: object.ID},
	}
	for _, test := range tests {
		t.Run(test.episodeID, func(t *testing.T) {
			episode, err := eng.IngestEpisode(ctx, EpisodeInput{
				ID: test.episodeID, Kind: "note", Content: "literal " + test.episodeID,
				Source: SourceInput{Kind: "note", ExternalRef: "namespace-" + test.episodeID},
			})
			if err != nil {
				t.Fatalf("ingest episode %q: %v", test.episodeID, err)
			}
			fact, err := eng.AssertFact(ctx, FactInput{
				ID: "namespace-" + test.episodeID, Predicate: "HAS_SCOPE", SubjectID: subject.ID,
				ObjectID: test.objectID, SupportingEpisodeIDs: []string{episode.EpisodeID},
			})
			if err != nil {
				t.Fatalf("assert fact for %q: %v", test.episodeID, err)
			}

			neighborhood, err := eng.Neighborhood(ctx, NeighborhoodRequest{AnchorIDs: []string{fact.ID}, MaxHops: 1})
			if err != nil {
				t.Fatalf("neighborhood: %v", err)
			}
			asserts := edgesOfType(neighborhood.Edges, "ASSERTS")
			if len(asserts) != 1 {
				t.Fatalf("expected one ASSERTS edge, got %#v", neighborhood.Edges)
			}
			if asserts[0].FromID != episode.EpisodeID || asserts[0].ToID != fact.ID {
				t.Fatalf("unexpected ASSERTS edge: %#v", asserts[0])
			}

			// The fact's literal endpoint is "subject", so the SUBJECT edge and the
			// ASSERTS edge must be told apart by their relationship type, not by
			// their endpoints alone.
			subjectEdges := edgesOfType(neighborhood.Edges, "SUBJECT")
			if len(subjectEdges) != 1 {
				t.Fatalf("expected one SUBJECT edge, got %#v", neighborhood.Edges)
			}
			if subjectEdges[0].FromID != fact.ID || subjectEdges[0].ToID != subject.ID {
				t.Fatalf("unexpected SUBJECT edge: %#v", subjectEdges[0])
			}
			if subjectEdges[0].ID == asserts[0].ID {
				t.Fatalf("SUBJECT and ASSERTS edges aliased on id %q", subjectEdges[0].ID)
			}

			if test.objectID == "" {
				return
			}
			objectEdges := edgesOfType(neighborhood.Edges, "OBJECT")
			if len(objectEdges) != 1 {
				t.Fatalf("expected one OBJECT edge, got %#v", neighborhood.Edges)
			}
			if objectEdges[0].FromID != fact.ID || objectEdges[0].ToID != object.ID {
				t.Fatalf("unexpected OBJECT edge: %#v", objectEdges[0])
			}
			if objectEdges[0].ID == asserts[0].ID {
				t.Fatalf("OBJECT and ASSERTS edges aliased on id %q", objectEdges[0].ID)
			}
		})
	}
}

// TestNeighborhoodSurvivesAliasedSubjectAndAssertsIDs builds the exact state
// the ambiguity produced: an entity and an episode sharing the explicit id
// "subject", plus a fact whose SUBJECT edge and ASSERTS edge were encoded to
// the same id before the type became part of edge identity. Both edges must be
// present with distinct ids. State is assembled directly to bypass the
// write-time uniqueness check that now rejects such a database at Open.
func TestNeighborhoodSurvivesAliasedSubjectAndAssertsIDs(t *testing.T) {
	eng, ctx := openIdentityTestEngine(t)
	now := eng.now()

	eng.mu.Lock()
	eng.sources["src-alias"] = Source{ID: "src-alias", SpaceID: "default", Kind: "note", CreatedAt: now}
	eng.episodes["subject"] = Episode{
		ID: "subject", SpaceID: "default", Kind: "note", Content: "subject",
		SourceID: "src-alias", IngestedAt: now, ObservedAt: now,
	}
	eng.entities["subject"] = Entity{ID: "subject", SpaceID: "default", Type: "Thing", CanonicalName: "Subject", CreatedAt: now, UpdatedAt: now}
	eng.facts["f"] = Fact{
		ID: "f", SpaceID: "default", Predicate: "HAS_SCOPE", SubjectID: "subject",
		Status: factStatusActive, SupportingEpisodeIDs: []string{"subject"},
		CreatedAt: now, UpdatedAt: now, ObservedAt: now,
	}
	eng.mu.Unlock()

	neighborhood, err := eng.Neighborhood(ctx, NeighborhoodRequest{AnchorIDs: []string{"f"}, MaxHops: 1})
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	subjectEdges := edgesOfType(neighborhood.Edges, "SUBJECT")
	asserts := edgesOfType(neighborhood.Edges, "ASSERTS")
	if len(subjectEdges) != 1 || len(asserts) != 1 {
		t.Fatalf("expected one SUBJECT and one ASSERTS edge, got %#v", neighborhood.Edges)
	}
	if subjectEdges[0].ID == asserts[0].ID {
		t.Fatalf("SUBJECT and ASSERTS edges aliased on id %q", subjectEdges[0].ID)
	}
	if subjectEdges[0].FromID != "f" || subjectEdges[0].ToID != "subject" {
		t.Fatalf("unexpected SUBJECT edge: %#v", subjectEdges[0])
	}
	if asserts[0].FromID != "subject" || asserts[0].ToID != "f" {
		t.Fatalf("unexpected ASSERTS edge: %#v", asserts[0])
	}
	// Pin the exact encoding so a call site that drops the relationship type
	// fails here instead of silently collapsing the two edges again.
	if want := compositeID("edge", "SUBJECT", "f", "subject"); subjectEdges[0].ID != want {
		t.Fatalf("SUBJECT edge id = %q, want %q", subjectEdges[0].ID, want)
	}
	if want := compositeID("edge", "ASSERTS", "subject", "f"); asserts[0].ID != want {
		t.Fatalf("ASSERTS edge id = %q, want %q", asserts[0].ID, want)
	}

	// Provenance edges use the same type-first identity.
	provenance, err := eng.Provenance(ctx, ProvenanceRequest{Kind: "episode", ID: "subject", MaxDepth: 1})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if want := compositeID("prov", "ASSERTS", "subject", "f"); !slices.ContainsFunc(provenance.Edges, func(edge ProvenanceEdge) bool {
		return edge.Type == "ASSERTS" && edge.ID == want
	}) {
		t.Fatalf("expected provenance ASSERTS edge %q, got %#v", want, provenance.Edges)
	}
	if want := compositeID("prov", "FROM_SOURCE", "subject", "src-alias"); !slices.ContainsFunc(provenance.Edges, func(edge ProvenanceEdge) bool {
		return edge.Type == "FROM_SOURCE" && edge.ID == want
	}) {
		t.Fatalf("expected provenance FROM_SOURCE edge %q, got %#v", want, provenance.Edges)
	}
}
