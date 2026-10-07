package yeoul

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMutationStateCopyOwnership(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: filepath.Join(t.TempDir(), "state.db"), CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close(ctx) })

	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "entity:copy-ownership",
		Type:          "Thing",
		CanonicalName: "copy ownership",
		Metadata:      map[string]any{"nested": map[string]any{"value": "original"}},
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	raw := eng.(*engine)
	raw.mu.Lock()
	defer raw.mu.Unlock()
	store := raw.store.(*ownershipStore).stateStore.(*latticeStore)

	borrowed := raw.stateLocked()
	borrowedEntity := borrowed.Entities[entity.ID]
	borrowedEntity.Metadata["nested"].(map[string]any)["value"] = "borrowed"
	borrowed.Entities[entity.ID] = borrowedEntity
	if got := raw.entities[entity.ID].Metadata["nested"].(map[string]any)["value"]; got != "borrowed" {
		t.Fatalf("stateLocked should borrow engine metadata, got %v", got)
	}
	if got := store.lastState.Entities[entity.ID].Metadata["nested"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("store lastState must be independently owned, got %v", got)
	}

	snapshot := raw.snapshotLocked()
	snapshotEntity := snapshot.Entities[entity.ID]
	snapshotEntity.Metadata["nested"].(map[string]any)["value"] = "snapshot"
	snapshot.Entities[entity.ID] = snapshotEntity
	if got := raw.entities[entity.ID].Metadata["nested"].(map[string]any)["value"]; got != "borrowed" {
		t.Fatalf("snapshot mutation leaked into engine state, got %v", got)
	}
}
