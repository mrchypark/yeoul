package yeoul

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestLegacySeededHistoryIsMarkedInferred proves the reconstructed legacy
// history is labelled instead of being presented as exact. The seeded initial
// revision carries the inferred marker, while the recorded current revision
// and a current (no as_of) read stay unmarked.
func TestLegacySeededHistoryIsMarkedInferred(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	asOfBeforeUpdate := createdAt.Add(time.Minute)

	// A legacy supersede->retract history: the fact was created active, later
	// superseded, and finally retracted. The seed can only reconstruct an
	// active initial value and a current retracted value, so the intermediate
	// superseded period is unknowable.
	rawEng.applyState(persistedState{
		Version: 1,
		Sources: map[string]Source{
			"src:legacy-history": {ID: "src:legacy-history", SpaceID: "default", Kind: "note", CreatedAt: createdAt},
		},
		Episodes: map[string]Episode{
			"ep:legacy-history": {ID: "ep:legacy-history", SpaceID: "default", Kind: "note", Content: "legacy", SourceID: "src:legacy-history", IngestedAt: createdAt},
		},
		Entities: map[string]Entity{
			"entity:legacy-history": {ID: "entity:legacy-history", SpaceID: "default", Type: "Thing", CanonicalName: "Legacy", CreatedAt: createdAt, UpdatedAt: createdAt},
		},
		Facts: map[string]Fact{
			"fact:legacy-history": {
				ID:                   "fact:legacy-history",
				SpaceID:              "default",
				Predicate:            "HAS_STATE",
				SubjectID:            "entity:legacy-history",
				ValueText:            "legacy state",
				Status:               factStatusRetracted,
				CreatedAt:            createdAt,
				UpdatedAt:            updatedAt,
				RetractedAt:          updatedAt,
				SupportingEpisodeIDs: []string{"ep:legacy-history"},
				Metadata:             map[string]any{"superseded_by": "fact:legacy-next"},
			},
		},
	})

	if _, ok := rawEng.migrationWatermarks[bitemporalWatermark]; !ok {
		t.Fatal("expected the bitemporal seed watermark")
	}
	watermark := rawEng.migrationWatermarks[bitemporalWatermark]
	if inferred, _ := watermark.Metadata["inferred_history"].(bool); !inferred {
		t.Fatalf("expected the watermark to disclose inferred history, got %#v", watermark.Metadata)
	}

	historical, err := eng.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{"entity:legacy-history"},
		Temporal:   TemporalFilter{AsOf: &asOfBeforeUpdate, IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Facts) != 1 {
		t.Fatalf("expected one historical fact, got %#v", historical.Facts)
	}
	if inferred, _ := historical.Facts[0].Metadata[historyInferredKey].(bool); !inferred {
		t.Fatalf("expected the reconstructed history to be marked inferred, got %#v", historical.Facts[0].Metadata)
	}

	// A current read is the recorded retracted fact, not a reconstruction, so it
	// must not carry the inferred marker.
	current, err := eng.GetFact(ctx, "fact:legacy-history")
	if err != nil {
		t.Fatalf("get current fact: %v", err)
	}
	if _, marked := current.Metadata[historyInferredKey]; marked {
		t.Fatalf("expected the recorded current fact to stay unmarked, got %#v", current.Metadata)
	}
}

// TestLegacySeededEntityRevisionIsMarkedInferred proves the entity half of the
// inferred-history disclosure. An entity revision has no status field, so its
// metadata is the only place the marker can live: the revision the legacy seed
// reconstructs carries it, while the live entity and an ordinary read do not.
func TestLegacySeededEntityRevisionIsMarkedInferred(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	rawEng.applyState(persistedState{
		Version: 1,
		Entities: map[string]Entity{
			"entity:legacy-entity-history": {
				ID:            "entity:legacy-entity-history",
				SpaceID:       "default",
				Type:          "Thing",
				CanonicalName: "Legacy Entity",
				Metadata:      map[string]any{"note": "legacy"},
				CreatedAt:     createdAt,
				UpdatedAt:     createdAt,
			},
		},
	})

	revision, ok := rawEng.entityRevisions["seed:entity:legacy-entity-history"]
	if !ok {
		t.Fatalf("expected a seeded entity revision, got %#v", rawEng.entityRevisions)
	}
	if inferred, _ := revision.Metadata[historyInferredKey].(bool); !inferred {
		t.Fatalf("expected the seeded entity revision to be marked inferred, got %#v", revision.Metadata)
	}

	live, err := eng.GetEntity(ctx, "entity:legacy-entity-history")
	if err != nil {
		t.Fatalf("get live entity: %v", err)
	}
	if _, marked := live.Metadata[historyInferredKey]; marked {
		t.Fatalf("expected the live entity to stay unmarked, got %#v", live.Metadata)
	}
	if live.Metadata["note"] != "legacy" {
		t.Fatalf("expected caller metadata to survive the seed, got %#v", live.Metadata)
	}

	// The reconstructed revision is what an as_of read before the recorded
	// update resolves to, so the marker must travel into the query result.
	asOf := createdAt.Add(time.Minute)
	historical, err := eng.GetRecord(ctx, GetRecordRequest{
		Kind:     "entity",
		ID:       "entity:legacy-entity-history",
		Temporal: TemporalFilter{AsOf: &asOf},
	})
	if err != nil {
		t.Fatalf("get historical entity: %v", err)
	}
	record, ok := historical.Record.(*Entity)
	if !ok {
		t.Fatalf("expected an entity record, got %T", historical.Record)
	}
	if inferred, _ := record.Metadata[historyInferredKey].(bool); !inferred {
		t.Fatalf("expected the reconstructed entity history to be marked inferred, got %#v", record.Metadata)
	}
}

// TestFreshSeededWatermarkDisclosesNoInferredHistory proves the watermark never
// claims inferred history when the seed reconstructed nothing. A fresh
// in-memory database still runs the seed (Open applies an empty state), so the
// flag and the counts must come from what the seed actually reconstructed.
func TestFreshSeededWatermarkDisclosesNoInferredHistory(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	watermark, ok := rawEng.migrationWatermarks[bitemporalWatermark]
	if !ok {
		t.Fatal("expected the bitemporal seed watermark on a fresh database")
	}
	if inferred, _ := watermark.Metadata["inferred_history"].(bool); inferred {
		t.Fatalf("expected no inferred history on a fresh database, got %#v", watermark.Metadata)
	}

	// Ordinary writes do not re-run the seed, so the watermark keeps reporting
	// zero reconstruction after real state exists.
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "fresh", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "fresh"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_STATE",
		SubjectID:            entity.ID,
		ValueText:            "fresh",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	watermark = rawEng.migrationWatermarks[bitemporalWatermark]
	if inferred, _ := watermark.Metadata["inferred_history"].(bool); inferred {
		t.Fatalf("expected the fresh watermark to stay false, got %#v", watermark.Metadata)
	}
	if got := watermarkInt(t, watermark.Metadata, "inferred_fact_count"); got != 0 {
		t.Fatalf("expected inferred_fact_count 0, got %d", got)
	}
	if got := watermarkInt(t, watermark.Metadata, "inferred_entity_count"); got != 0 {
		t.Fatalf("expected inferred_entity_count 0, got %d", got)
	}
}

// TestUpsertEntityRejectsLifecycleManagedMetadata proves an entity cannot
// smuggle the engine-managed inferred marker into stored history semantics and
// that the rejected write leaves the stored entity untouched.
func TestUpsertEntityRejectsLifecycleManagedMetadata(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	original, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "entity:managed-metadata",
		Type:          "Thing",
		CanonicalName: "Managed",
		Metadata:      map[string]any{"note": "keep"},
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	revisionsBefore := len(rawEng.entityRevisions)

	_, err = eng.UpsertEntity(ctx, EntityInput{
		ID:            original.ID,
		Type:          "Thing",
		CanonicalName: "Managed Renamed",
		Metadata:      map[string]any{historyInferredKey: true, "note": "changed"},
	})
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a structured Yeoul error, got %v", err)
	}
	if yeoulErr.Code != ErrLifecycleInvalid {
		t.Fatalf("expected %s, got %s", ErrLifecycleInvalid, yeoulErr.Code)
	}
	if got := yeoulErr.Details["metadata_key"]; got != historyInferredKey {
		t.Fatalf("expected the rejected metadata key %q, got %#v", historyInferredKey, got)
	}

	stored, err := eng.GetEntity(ctx, original.ID)
	if err != nil {
		t.Fatalf("get stored entity: %v", err)
	}
	if stored.CanonicalName != "Managed" {
		t.Fatalf("expected the rejected write to leave the name unchanged, got %q", stored.CanonicalName)
	}
	if _, marked := stored.Metadata[historyInferredKey]; marked {
		t.Fatalf("expected no inferred marker on the stored entity, got %#v", stored.Metadata)
	}
	if stored.Metadata["note"] != "keep" {
		t.Fatalf("expected the stored metadata to stay unchanged, got %#v", stored.Metadata)
	}
	if !stored.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("expected the rejected write to leave updated_at unchanged, got %v want %v", stored.UpdatedAt, original.UpdatedAt)
	}
	if got := len(rawEng.entityRevisions); got != revisionsBefore {
		t.Fatalf("expected no revision from the rejected write, got %d want %d", got, revisionsBefore)
	}
}

// watermarkInt reads a numeric watermark field. An in-memory seed stores Go
// ints, while a watermark decoded from a persisted store yields float64.
func watermarkInt(t *testing.T, metadata map[string]any, key string) int {
	t.Helper()
	switch value := metadata[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		t.Fatalf("expected %s to be a number, got %#v", key, metadata[key])
		return 0
	}
}
