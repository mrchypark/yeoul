package yeoul

import (
	"slices"
	"testing"
)

func hasErrorCode(err error, code ErrorCode) bool {
	structured := unwrapYeoulError(err)
	return structured != nil && structured.Code == code
}

// seedResolveEntities installs entities directly on the engine so the test can
// pin exact namespaces, types, aliases, and stable keys that the ordinary write
// path would normalize.
func seedResolveEntities(t *testing.T, e *engine, entities ...Entity) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	for _, entity := range entities {
		if entity.SpaceID == "" {
			entity.SpaceID = "default"
		}
		entity.CreatedAt = now
		entity.UpdatedAt = now
		e.entities[entity.ID] = entity
	}
}

func resolveIDs(resp *EntityResolveResponse) []string {
	ids := make([]string, 0, len(resp.Matches))
	for _, entity := range resp.Matches {
		ids = append(ids, entity.ID)
	}
	return ids
}

func resolveDriftedIDs(resp *EntityResolveResponse) []string {
	ids := make([]string, 0, len(resp.Drifted))
	for _, entity := range resp.Drifted {
		ids = append(ids, entity.ID)
	}
	return ids
}

func TestResolveEntityExactMatch(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "repo",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "repo", Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the exact entity, got %v", got)
	}
	if len(resp.Drifted) != 0 {
		t.Fatalf("expected no drift on an exact match, got %v", resolveDriftedIDs(resp))
	}
}

func TestResolveEntityFoldedNamespaceAndTypeAreDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "Repo",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "repo", Type: "project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the folded match, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected a folded namespace/type match to be reported as drift, got %v", got)
	}
}

func TestResolveEntityBlankNamespaceIsToleratedDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})

	// Populated request against a blank stored namespace.
	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "project:yeoul", Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the blank-namespace entity to match, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected a blank-vs-populated namespace to be drift, got %v", got)
	}

	// Blank request against a populated stored namespace.
	seedResolveEntities(t, e, Entity{
		ID:            "project:populated",
		Namespace:     "repo",
		Type:          "Project",
		CanonicalName: "Yeoul Populated",
	})
	resp, err = e.ResolveEntity(ctx, EntityResolveRequest{Type: "Project", CanonicalName: "Yeoul Populated"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:populated"}) {
		t.Fatalf("expected the populated-namespace entity to match a blank request, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:populated"}) {
		t.Fatalf("expected the blank-vs-populated match to be drift, got %v", got)
	}
}

func TestResolveEntityAliasMatchIsDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "repo",
		Type:          "Project",
		CanonicalName: "Yeoul",
		Aliases:       []string{"yeoul-memory", "Project Yeoul"},
	})

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "repo", Type: "Project", CanonicalName: "yeoul-memory"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the alias to match, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected an alias match to be reported as drift, got %v", got)
	}
}

func TestResolveEntityDistinctStableKeysNeverMatch(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "person:alpha",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"stable_key": "alpha"},
	})

	_, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex", StableKey: "beta"})
	if !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected a distinct stable key to miss, got %v", err)
	}
}

func TestResolveEntityStableKeyAndNoKeyNeverMatch(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "person:keyed",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"stable_key": "alpha"},
	})

	// A request with no key must not match a keyed entity on the name alone.
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex"}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected an unkeyed request to miss a keyed entity, got %v", err)
	}
	// A request with a key must not match an unkeyed entity.
	seedResolveEntities(t, e, Entity{
		ID:            "person:unkeyed",
		Type:          "Person",
		CanonicalName: "Sam",
	})
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Sam", StableKey: "gamma"}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected a keyed request to miss an unkeyed entity, got %v", err)
	}
}

func TestResolveEntityStableKeyIsTheIdentityWhenBothSidesCarryOne(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "person:keyed",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"stable_key": "alpha"},
	})

	// A strong key is compared exactly and the display name is not consulted,
	// matching the engine's stored identity rule.
	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Some Other Name", StableKey: "alpha"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"person:keyed"}) {
		t.Fatalf("expected the stable key to decide the identity, got %v", got)
	}
}

func TestResolveEntitySpaceFilter(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:other",
		SpaceID:       "other",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})

	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{SpaceID: "default", Type: "Project", CanonicalName: "Yeoul"}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected the space filter to exclude another space, got %v", err)
	}
	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{SpaceID: "other", Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("resolve entity in its own space: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:other"}) {
		t.Fatalf("expected the entity in its own space, got %v", got)
	}
}

func TestResolveEntityRejectsEmptyType(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{CanonicalName: "Yeoul"}); !hasErrorCode(err, ErrInputInvalid) {
		t.Fatalf("expected an empty type to be rejected, got %v", err)
	}
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Project"}); !hasErrorCode(err, ErrInputInvalid) {
		t.Fatalf("expected a missing name and key to be rejected, got %v", err)
	}
}

func TestResolveEntityNoMatchReturnsNotFound(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{ID: "project:yeoul", Type: "Project", CanonicalName: "Yeoul"})

	_, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Project", CanonicalName: "Missing"})
	if !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected ErrEntityNotFound, got %v", err)
	}
	var apiErr *Error
	if apiErr = unwrapYeoulError(err); apiErr == nil {
		t.Fatalf("expected a structured error, got %T", err)
	}
	for _, field := range []string{"namespace", "type", "canonical_name", "stable_key", "space_id"} {
		if _, ok := apiErr.Details[field]; !ok {
			t.Fatalf("expected details to carry %q, got %v", field, apiErr.Details)
		}
	}
}

func TestResolveEntitySortsMatchesByID(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e,
		Entity{ID: "person:charlie", Type: "Person", CanonicalName: "Alex"},
		Entity{ID: "person:alpha", Type: "Person", CanonicalName: "Alex"},
		Entity{ID: "person:bravo", Type: "Person", CanonicalName: "Alex"},
	)

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	want := []string{"person:alpha", "person:bravo", "person:charlie"}
	if got := resolveIDs(resp); !slices.Equal(got, want) {
		t.Fatalf("expected matches sorted by ID %v, got %v", want, got)
	}
}

func TestResolveEntityExcludesMarkedDuplicates(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e,
		Entity{ID: "person:alpha", Type: "Person", CanonicalName: "Alex"},
		Entity{ID: "person:duplicate", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"duplicate_of": "person:alpha"}},
	)

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"person:alpha"}) {
		t.Fatalf("expected only the canonical entity, got %v", got)
	}
}

func TestResolveEntityCanIncludeMarkedDuplicatesForGuards(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "person:duplicate",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"duplicate_of": "person:canonical"},
	})
	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex", IncludeMarkedDuplicates: true})
	if err != nil {
		t.Fatalf("resolve marked duplicate: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"person:duplicate"}) {
		t.Fatalf("expected marked duplicate in guard resolution, got %v", got)
	}
}

func TestResolveEntityFoldedCanonicalNameIsDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "repo",
		Type:          "Project",
		CanonicalName: "Yeoul",
	})

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "repo", Type: "Project", CanonicalName: "yeoul"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the folded canonical name to match, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected a folded canonical name to be reported as drift, got %v", got)
	}
}

func TestResolveEntityFoldedAliasIsDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "project:yeoul",
		Namespace:     "repo",
		Type:          "Project",
		CanonicalName: "Yeoul",
		Aliases:       []string{"Yeoul Memory"},
	})

	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Namespace: "repo", Type: "Project", CanonicalName: "yeoul memory"})
	if err != nil {
		t.Fatalf("resolve entity: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected the folded alias to match, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"project:yeoul"}) {
		t.Fatalf("expected a folded alias to be reported as drift, got %v", got)
	}
}

func TestResolveEntityKeyDriftRequiresOptIn(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e,
		Entity{ID: "person:keyed", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"stable_key": "alpha"}},
		Entity{ID: "person:unkeyed", Type: "Person", CanonicalName: "Sam"},
	)

	// A keyed request against an unkeyed entity misses by default.
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Sam", StableKey: "gamma"}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected a keyed request to miss an unkeyed entity by default, got %v", err)
	}
	resp, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Sam", StableKey: "gamma", IncludeKeyDrift: true})
	if err != nil {
		t.Fatalf("resolve entity with IncludeKeyDrift: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"person:unkeyed"}) {
		t.Fatalf("expected the unkeyed entity to match with IncludeKeyDrift, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"person:unkeyed"}) {
		t.Fatalf("expected the keyed-vs-unkeyed match to be drift, got %v", got)
	}

	// Reverse: an unkeyed request against a keyed entity.
	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex"}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected an unkeyed request to miss a keyed entity by default, got %v", err)
	}
	resp, err = e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex", IncludeKeyDrift: true})
	if err != nil {
		t.Fatalf("resolve entity with IncludeKeyDrift: %v", err)
	}
	if got := resolveIDs(resp); !slices.Equal(got, []string{"person:keyed"}) {
		t.Fatalf("expected the keyed entity to match with IncludeKeyDrift, got %v", got)
	}
	if got := resolveDriftedIDs(resp); !slices.Equal(got, []string{"person:keyed"}) {
		t.Fatalf("expected the unkeyed-vs-keyed match to be drift, got %v", got)
	}
}

func TestResolveEntityDistinctStableKeysNeverMatchWithKeyDrift(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	seedResolveEntities(t, e, Entity{
		ID:            "person:keyed",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"stable_key": "alpha"},
	})

	if _, err := e.ResolveEntity(ctx, EntityResolveRequest{Type: "Person", CanonicalName: "Alex", StableKey: "beta", IncludeKeyDrift: true}); !hasErrorCode(err, ErrEntityNotFound) {
		t.Fatalf("expected two different strong keys to stay distinct even with IncludeKeyDrift, got %v", err)
	}
}
