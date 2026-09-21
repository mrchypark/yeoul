package yeoul

import (
	"slices"
	"testing"
)

// seedCanonicalPair installs a canonical entity plus a duplicate marked with
// duplicate_of, and a fact stored against the duplicate. The read paths must
// surface that fact when the caller filters by the canonical entity.
func seedCanonicalPair(t *testing.T, e *engine) (*Entity, *Entity, *Fact) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	canonical := Entity{
		ID:            "project:yeoul",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	duplicate := Entity{
		ID:            "project:yeoul-drift",
		SpaceID:       "default",
		Type:          "Project",
		CanonicalName: "Yeoul",
		Metadata:      map[string]any{"duplicate_of": "project:yeoul"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	e.entities[canonical.ID] = canonical
	e.entities[duplicate.ID] = duplicate

	fact := Fact{
		ID:         "fact:uses",
		SpaceID:    "default",
		Predicate:  "USES",
		SubjectID:  duplicate.ID,
		ObjectID:   duplicate.ID,
		ValueText:  "attached to the duplicate",
		Status:     factStatusActive,
		ObservedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata:   map[string]any{},
	}
	e.facts[fact.ID] = fact
	return &canonical, &duplicate, &fact
}

func TestLookupFactsResolvesMergedDuplicateSubject(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, duplicate, fact := seedCanonicalPair(t, e)
	if canonical.ID == duplicate.ID {
		t.Fatal("expected distinct canonical and duplicate IDs")
	}

	resp, err := e.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{canonical.ID}})
	if err != nil {
		t.Fatalf("lookup facts: %v", err)
	}
	if len(resp.Facts) != 1 || resp.Facts[0].ID != fact.ID {
		t.Fatalf("expected the duplicate's fact to be reachable from the canonical subject, got %+v", resp.Facts)
	}

	// The stored fact is unchanged: this is a read-time view only.
	stored, err := e.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if stored.SubjectID != duplicate.ID {
		t.Fatalf("expected the stored subject to stay %q, got %q", duplicate.ID, stored.SubjectID)
	}
}

func TestLookupFactsResolvesMergedDuplicateObject(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, _, fact := seedCanonicalPair(t, e)

	resp, err := e.LookupFacts(ctx, FactLookupRequest{ObjectIDs: []string{canonical.ID}})
	if err != nil {
		t.Fatalf("lookup facts: %v", err)
	}
	if len(resp.Facts) != 1 || resp.Facts[0].ID != fact.ID {
		t.Fatalf("expected the duplicate's fact to be reachable from the canonical object, got %+v", resp.Facts)
	}
}

func TestNeighborhoodReachesCanonicalEntityFromDuplicateFact(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, _, fact := seedCanonicalPair(t, e)

	resp, err := e.Neighborhood(ctx, NeighborhoodRequest{AnchorIDs: []string{canonical.ID}, MaxHops: 1})
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	if !slices.ContainsFunc(resp.Nodes, func(node GraphNode) bool { return node.ID == fact.ID }) {
		t.Fatalf("expected the fact node reachable from the canonical anchor, got %+v", resp.Nodes)
	}
	if !slices.ContainsFunc(resp.Edges, func(edge GraphEdge) bool { return edge.ToID == canonical.ID && edge.FromID == fact.ID }) {
		t.Fatalf("expected an edge into the canonical entity, got %+v", resp.Edges)
	}
}

func TestProvenanceReachesFactsOfMergedDuplicate(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, _, fact := seedCanonicalPair(t, e)

	resp, err := e.Provenance(ctx, ProvenanceRequest{Kind: "entity", ID: canonical.ID, MaxDepth: 1})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if !slices.ContainsFunc(resp.Nodes, func(node ProvenanceNode) bool { return node.ID == fact.ID }) {
		t.Fatalf("expected the duplicate's fact in the canonical provenance, got %+v", resp.Nodes)
	}
	if !slices.ContainsFunc(resp.Edges, func(edge ProvenanceEdge) bool { return edge.ToID == canonical.ID && edge.FromID == fact.ID }) {
		t.Fatalf("expected a provenance edge into the canonical entity, got %+v", resp.Edges)
	}
}

func TestTimelineReachesFactsOfMergedDuplicate(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, _, fact := seedCanonicalPair(t, e)

	resp, err := e.Timeline(ctx, TimelineRequest{AnchorIDs: []string{canonical.ID}})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if !slices.ContainsFunc(resp.Events, func(event TimelineEvent) bool { return event.RecordID == fact.ID }) {
		t.Fatalf("expected the duplicate's fact on the canonical timeline, got %+v", resp.Events)
	}
}

func TestCanonicalEntityIDLockedStopsOnCycles(t *testing.T) {
	e, _ := openIdentityTestEngine(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entities["a"] = Entity{ID: "a", Metadata: map[string]any{"duplicate_of": "b"}}
	e.entities["b"] = Entity{ID: "b", Metadata: map[string]any{"duplicate_of": "a"}}

	got := e.canonicalEntityIDLocked("a")
	if got != "a" && got != "b" {
		t.Fatalf("expected the cycle walk to stop on a member, got %q", got)
	}
	if got := e.canonicalEntityIDLocked("missing"); got != "missing" {
		t.Fatalf("expected an unknown id to pass through, got %q", got)
	}
	if got := e.canonicalEntityIDLocked(""); got != "" {
		t.Fatalf("expected a blank id to pass through, got %q", got)
	}
}
