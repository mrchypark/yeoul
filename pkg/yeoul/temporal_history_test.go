package yeoul

import (
	"context"
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
