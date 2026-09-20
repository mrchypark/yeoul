package yeoul

import (
	"context"
	"testing"
)

func openIdentityTestEngine(t *testing.T) (*engine, context.Context) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open in-memory engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close(ctx) })
	e, ok := eng.(*engine)
	if !ok {
		t.Fatalf("expected *engine, got %T", eng)
	}
	return e, ctx
}

func TestBatchBindsFactReferenceToReusedLegacyEntity(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	legacyID := legacyEntityID("", "DecisionTopic", "Structured memory promotion")
	e.mu.Lock()
	e.entities[legacyID] = Entity{
		ID:            legacyID,
		SpaceID:       "default",
		Type:          "DecisionTopic",
		CanonicalName: "Structured memory promotion",
		CreatedAt:     e.now(),
		UpdatedAt:     e.now(),
	}
	e.mu.Unlock()

	derived := EntityID("", "DecisionTopic", "Structured memory promotion")
	if derived == legacyID {
		t.Fatal("expected the derived ID to differ from the legacy ID")
	}

	result, err := e.IngestBatch(ctx, BatchInput{
		Episodes: []EpisodeInput{{
			ID:      "ep-batch",
			Kind:    "note",
			Content: "batch",
			Source:  SourceInput{Kind: "note", ExternalRef: "batch"},
		}},
		Entities: []EntityInput{{Type: "DecisionTopic", CanonicalName: "Structured memory promotion"}},
		Facts: []FactInput{{
			ID:                   "fact-batch",
			Predicate:            "HAS_DECISION",
			SubjectID:            derived,
			SupportingEpisodeIDs: []string{"ep-batch"},
		}},
	})
	if err != nil {
		t.Fatalf("ingest batch: %v", err)
	}
	if len(result.EntityIDs) != 1 || result.EntityIDs[0] != legacyID {
		t.Fatalf("expected the legacy entity to be reused, got %v", result.EntityIDs)
	}
	fact, err := e.GetFact(ctx, "fact-batch")
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if fact.SubjectID != legacyID {
		t.Fatalf("expected the fact to reference the reused legacy entity %q, got %q", legacyID, fact.SubjectID)
	}
}

func TestEntityStableKeyMetadataCannotBeOverwritten(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	first, err := e.UpsertEntity(ctx, EntityInput{Type: "Feature", CanonicalName: "key", StableKey: "key"})
	if err != nil {
		t.Fatalf("create keyed entity: %v", err)
	}

	if _, err := e.UpsertEntity(ctx, EntityInput{
		Type:          "Feature",
		CanonicalName: "key",
		Metadata:      map[string]any{"stable_key": "other"},
	}); err == nil {
		t.Fatal("expected a metadata-only stable key change to be rejected")
	}

	renamed, err := e.UpsertEntity(ctx, EntityInput{Type: "Feature", CanonicalName: "Renamed", StableKey: "key"})
	if err != nil {
		t.Fatalf("rename with the same stable key: %v", err)
	}
	if renamed.ID != first.ID {
		t.Fatalf("expected the same entity ID %q, got %q", first.ID, renamed.ID)
	}
	if got := metadataStableKey(renamed.Metadata); got != "key" {
		t.Fatalf("expected the stored stable key to stay %q, got %q", "key", got)
	}
}

func TestLegacyKeyedEntityRenameReusesStoredID(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	legacyID := legacyEntityID("", "Project", "project-123")
	e.mu.Lock()
	e.entities[legacyID] = Entity{
		ID:            legacyID,
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Old Name",
		Metadata:      map[string]any{"stable_key": "project-123"},
		CreatedAt:     e.now(),
		UpdatedAt:     e.now(),
	}
	e.mu.Unlock()

	updated, err := e.UpsertEntity(ctx, EntityInput{Type: "Project", CanonicalName: "New Name", StableKey: "project-123"})
	if err != nil {
		t.Fatalf("rename legacy keyed entity: %v", err)
	}
	if updated.ID != legacyID {
		t.Fatalf("expected the legacy ID %q to be reused, got %q", legacyID, updated.ID)
	}
	if updated.CanonicalName != "New Name" {
		t.Fatalf("expected the renamed canonical name, got %q", updated.CanonicalName)
	}
}

func TestEpisodeReplayReusesBaseVersionSourceID(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	baseID := legacySourceID("default", "note", "same")
	e.mu.Lock()
	e.sources[baseID] = Source{ID: baseID, SpaceID: "default", Kind: "note", ExternalRef: "same", CreatedAt: e.now()}
	e.mu.Unlock()

	result, err := e.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-replay",
		Kind:    "note",
		Content: "same",
		Source:  SourceInput{Kind: "note", ExternalRef: "same"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if result.SourceID != baseID {
		t.Fatalf("expected the base-version source ID %q, got %q", baseID, result.SourceID)
	}

	replay, err := e.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-replay",
		Kind:    "note",
		Content: "same",
		Source:  SourceInput{Kind: "note", ExternalRef: "same"},
	})
	if err != nil {
		t.Fatalf("replay episode: %v", err)
	}
	if replay.Created {
		t.Fatal("expected identical replay to return Created=false")
	}
}
