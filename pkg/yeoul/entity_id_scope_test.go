package yeoul

import (
	"context"
	"errors"
	"testing"
)

// TestDerivedEntityIDIsGlobalAcrossSpaces pins the documented identity scope:
// the generated entity ID is derived from namespace, type, and key only, so the
// same identity in two spaces collides and the second upsert fails closed
// instead of silently creating a space-local entity.
func TestDerivedEntityIDIsGlobalAcrossSpaces(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	first, err := eng.UpsertEntity(ctx, EntityInput{SpaceID: "alpha", Namespace: "repo", Type: "Repository", CanonicalName: "shared"})
	if err != nil {
		t.Fatalf("upsert in alpha: %v", err)
	}
	second, err := eng.UpsertEntity(ctx, EntityInput{SpaceID: "beta", Namespace: "repo", Type: "Repository", CanonicalName: "shared"})
	if err == nil {
		t.Fatalf("expected the second space to collide on the derived ID, got entity %#v", second)
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a structured Yeoul error, got %v", err)
	}
	if yeoulErr.Code != ErrLifecycleInvalid {
		t.Fatalf("expected %s, got %s", ErrLifecycleInvalid, yeoulErr.Code)
	}
	if got := yeoulErr.Details["entity_id"]; got != first.ID {
		t.Fatalf("expected the colliding derived ID %q, got %#v", first.ID, got)
	}
	if first.ID != EntityID("repo", "Repository", "shared") {
		t.Fatalf("expected the documented derived ID, got %q", first.ID)
	}
}

// TestExplicitEntityIDsStayGlobalAcrossSpaces pins the escape hatch: a caller
// that needs the same identity in two spaces must supply distinct explicit IDs
// or namespaces.
func TestExplicitEntityIDsStayGlobalAcrossSpaces(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{SpaceID: "alpha", ID: "repo:alpha:shared", Namespace: "repo", Type: "Repository", CanonicalName: "shared"}); err != nil {
		t.Fatalf("upsert alpha: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{SpaceID: "beta", ID: "repo:beta:shared", Namespace: "repo", Type: "Repository", CanonicalName: "shared"}); err != nil {
		t.Fatalf("expected distinct explicit IDs to coexist, got %v", err)
	}
}
