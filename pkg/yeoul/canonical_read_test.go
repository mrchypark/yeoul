package yeoul

import (
	"slices"
	"testing"
	"time"
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

func TestRawDuplicateAnchorsUseCanonicalReadView(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	_, duplicate, fact := seedCanonicalPair(t, e)

	neighborhood, err := e.Neighborhood(ctx, NeighborhoodRequest{AnchorIDs: []string{duplicate.ID}, MaxHops: 1})
	if err != nil || !slices.ContainsFunc(neighborhood.Nodes, func(node GraphNode) bool { return node.ID == fact.ID }) {
		t.Fatalf("expected raw duplicate anchor to reach fact, response=%+v err=%v", neighborhood, err)
	}
	timeline, err := e.Timeline(ctx, TimelineRequest{AnchorIDs: []string{duplicate.ID}})
	if err != nil || !slices.ContainsFunc(timeline.Events, func(event TimelineEvent) bool { return event.RecordID == fact.ID }) {
		t.Fatalf("expected raw duplicate anchor on timeline, response=%+v err=%v", timeline, err)
	}
	search, err := e.Search(ctx, SearchRequest{QueryText: "attached", Types: []string{"fact"}, AnchorIDs: []string{duplicate.ID}, Mode: SearchModeKeyword})
	if err != nil || !slices.ContainsFunc(search.Hits, func(hit SearchHit) bool { return hit.RecordID == fact.ID }) {
		t.Fatalf("expected raw duplicate anchor in search, response=%+v err=%v", search, err)
	}
}

func TestInvalidDuplicateRedirectDoesNotExposeFacts(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, duplicate, _ := seedCanonicalPair(t, e)
	e.mu.Lock()
	duplicate.Metadata["duplicate_of"] = "ghost"
	e.entities[duplicate.ID] = *duplicate
	e.entities["cross-space"] = Entity{ID: "cross-space", SpaceID: "other"}
	duplicate.Metadata["duplicate_of"] = "cross-space"
	e.entities[duplicate.ID] = *duplicate
	e.mu.Unlock()

	if got := e.canonicalEntityIDLocked(duplicate.ID); got != duplicate.ID {
		t.Fatalf("invalid redirect should preserve raw anchor, got %q", got)
	}
	resp, err := e.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{canonical.ID}})
	if err != nil {
		t.Fatalf("lookup facts: %v", err)
	}
	if len(resp.Facts) != 0 {
		t.Fatalf("cross-space redirect must not expose duplicate facts, got %+v", resp.Facts)
	}
}

func TestCanonicalReadsRespectHistoricalMergeState(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, duplicate, fact := seedCanonicalPair(t, e)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	duplicate.Metadata = map[string]any{"duplicate_of": canonical.ID}
	duplicate.UpdatedAt = before.Add(time.Hour)
	e.mu.Lock()
	storedFact := e.facts[fact.ID]
	storedFact.CreatedAt = before.Add(15 * time.Minute)
	storedFact.UpdatedAt = storedFact.CreatedAt
	storedFact.ObservedAt = storedFact.CreatedAt
	e.facts[fact.ID] = storedFact
	e.entityRevisions["entityrev-before-merge"] = EntityRevision{
		ID:            "entityrev-before-merge",
		EntityID:      duplicate.ID,
		SpaceID:       duplicate.SpaceID,
		RevisionKind:  "assert",
		TxTime:        before,
		Type:          duplicate.Type,
		CanonicalName: duplicate.CanonicalName,
		CreatedAt:     before.Add(-time.Hour),
		UpdatedAt:     before,
	}
	e.entities[duplicate.ID] = *duplicate
	e.mu.Unlock()

	old := before.Add(30 * time.Minute)
	oldResp, err := e.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{canonical.ID},
		Temporal:   TemporalFilter{AsOf: &old},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(oldResp.Facts) != 0 {
		t.Fatalf("pre-merge canonical anchor must not see later duplicate fact, got %+v", oldResp.Facts)
	}

	newResp, err := e.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{canonical.ID}})
	if err != nil {
		t.Fatalf("current lookup: %v", err)
	}
	if len(newResp.Facts) != 1 || newResp.Facts[0].ID != fact.ID {
		t.Fatalf("post-merge canonical anchor should see duplicate fact, got %+v", newResp.Facts)
	}
}

func TestCanonicalEntityIDLockedStopsOnCycles(t *testing.T) {
	e, _ := openIdentityTestEngine(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entities["a"] = Entity{ID: "a", Metadata: map[string]any{"duplicate_of": "b"}}
	e.entities["b"] = Entity{ID: "b", Metadata: map[string]any{"duplicate_of": "a"}}

	got := e.canonicalEntityIDLocked("a")
	if got != "a" {
		t.Fatalf("expected a cycle to fail closed to the raw anchor, got %q", got)
	}
	if got := e.canonicalEntityIDLocked("missing"); got != "missing" {
		t.Fatalf("expected an unknown id to pass through, got %q", got)
	}
	if got := e.canonicalEntityIDLocked(""); got != "" {
		t.Fatalf("expected a blank id to pass through, got %q", got)
	}
}

func TestIncludedRelatedEntitiesUseTemporalCanonicalEndpoints(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, duplicate, fact := seedCanonicalPair(t, e)
	mergeAt := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	e.mu.Lock()
	storedFact := e.facts[fact.ID]
	storedFact.CreatedAt = mergeAt.Add(15 * time.Minute)
	storedFact.UpdatedAt = storedFact.CreatedAt
	storedFact.ObservedAt = storedFact.CreatedAt
	e.facts[fact.ID] = storedFact
	duplicate.UpdatedAt = mergeAt.Add(time.Hour)
	e.entities[duplicate.ID] = *duplicate
	e.entityRevisions["entityrev-related-before-merge"] = EntityRevision{
		ID: "entityrev-related-before-merge", EntityID: duplicate.ID, SpaceID: duplicate.SpaceID,
		RevisionKind: "assert", TxTime: mergeAt, Type: duplicate.Type,
		CanonicalName: duplicate.CanonicalName, CreatedAt: mergeAt.Add(-time.Hour), UpdatedAt: mergeAt,
	}
	e.mu.Unlock()

	current, err := e.LookupFacts(ctx, FactLookupRequest{
		Include: Include{SupportingFacts: true, RelatedEntities: true},
	})
	if err != nil {
		t.Fatalf("current lookup: %v", err)
	}
	if len(current.Included.Entities) != 1 || current.Included.Entities[0].ID != canonical.ID {
		t.Fatalf("expected one current canonical related entity, got %+v", current.Included.Entities)
	}
	if len(current.Included.Facts) != 1 || current.Included.Facts[0].SubjectID != duplicate.ID || current.Included.Facts[0].ObjectID != duplicate.ID {
		t.Fatalf("included fact endpoints must remain raw, got %+v", current.Included.Facts)
	}

	old := mergeAt.Add(30 * time.Minute)
	historical, err := e.LookupFacts(ctx, FactLookupRequest{
		Temporal: TemporalFilter{AsOf: &old},
		Include:  Include{SupportingFacts: true, RelatedEntities: true},
	})
	if err != nil {
		t.Fatalf("historical lookup: %v", err)
	}
	if len(historical.Included.Entities) != 1 || historical.Included.Entities[0].ID != duplicate.ID {
		t.Fatalf("expected the pre-merge raw endpoint at historical time, got %+v", historical.Included.Entities)
	}
}

func TestIncludedRelatedEntitiesRejectInvalidCanonicalTarget(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	_, duplicate, fact := seedCanonicalPair(t, e)
	e.mu.Lock()
	duplicate.Metadata["duplicate_of"] = "cross-space"
	e.entities[duplicate.ID] = *duplicate
	e.entities["cross-space"] = Entity{ID: "cross-space", SpaceID: "other", Type: "Project", CanonicalName: "Other"}
	e.mu.Unlock()

	resp, err := e.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{duplicate.ID}, Include: Include{RelatedEntities: true},
	})
	if err != nil {
		t.Fatalf("lookup invalid redirect: %v", err)
	}
	if len(resp.Facts) != 1 || resp.Facts[0].ID != fact.ID {
		t.Fatalf("expected the raw fact to remain queryable, got %+v", resp.Facts)
	}
	if len(resp.Included.Entities) != 0 {
		t.Fatalf("invalid cross-space target must not be included, got %+v", resp.Included.Entities)
	}
}

func TestRecordPassesSearchFiltersCanonicalizesEntityAnchorsAtTemporalScope(t *testing.T) {
	e, ctx := openIdentityTestEngine(t)
	canonical, duplicate, _ := seedCanonicalPair(t, e)
	currentReq := SearchRequest{AnchorIDs: []string{duplicate.ID}}
	if !RecordPassesSearchFilters(ctx, e, canonical, currentReq) {
		t.Fatal("expected a raw duplicate anchor to match the canonical entity")
	}
	if len(currentReq.AnchorIDs) != 1 || currentReq.AnchorIDs[0] != duplicate.ID {
		t.Fatalf("filter helper mutated caller anchors: %#v", currentReq.AnchorIDs)
	}

	mergeAt := time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC)
	e.mu.Lock()
	duplicate.UpdatedAt = mergeAt.Add(time.Hour)
	e.entities[duplicate.ID] = *duplicate
	e.entityRevisions["entityrev-filter-before-merge"] = EntityRevision{
		ID: "entityrev-filter-before-merge", EntityID: duplicate.ID, SpaceID: duplicate.SpaceID,
		RevisionKind: "assert", TxTime: mergeAt, Type: duplicate.Type,
		CanonicalName: duplicate.CanonicalName, CreatedAt: mergeAt.Add(-time.Hour), UpdatedAt: mergeAt,
	}
	unrelated := Entity{ID: "project:unrelated", SpaceID: "default", Type: "Project", CanonicalName: "Unrelated"}
	e.entities[unrelated.ID] = unrelated
	e.mu.Unlock()

	old := mergeAt.Add(30 * time.Minute)
	if RecordPassesSearchFilters(ctx, e, canonical, SearchRequest{Temporal: TemporalFilter{AsOf: &old}, AnchorIDs: []string{duplicate.ID}}) {
		t.Fatal("pre-merge canonical entity must not match a duplicate anchor")
	}
	if !RecordPassesSearchFilters(ctx, e, duplicate, SearchRequest{Temporal: TemporalFilter{AsOf: &old}, AnchorIDs: []string{duplicate.ID}}) {
		t.Fatal("pre-merge duplicate entity must match its raw anchor")
	}
	if RecordPassesSearchFilters(ctx, e, &unrelated, currentReq) {
		t.Fatal("unrelated entity must remain excluded")
	}
}
