package yeoul

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// requireFactConflict asserts err is the single-value slot conflict and returns
// its structured form. The occupant IDs are compared exactly, so the sorted,
// deterministic list is part of the contract being pinned.
func requireFactConflict(t *testing.T, err error, wantOccupants []string) *Error {
	t.Helper()
	var yeErr *Error
	if !errors.As(err, &yeErr) {
		t.Fatalf("expected structured conflict error, got %v", err)
	}
	if yeErr.Code != ErrFactConflict {
		t.Fatalf("expected %s, got %s", ErrFactConflict, yeErr.Code)
	}
	if yeErr.Message != "single-value fact slot is already occupied" {
		t.Fatalf("unexpected conflict message %q", yeErr.Message)
	}
	got, ok := yeErr.Details["conflicting_fact_ids"].([]string)
	if !ok {
		t.Fatalf("expected conflicting_fact_ids list, got %#v", yeErr.Details["conflicting_fact_ids"])
	}
	if !slices.Equal(got, wantOccupants) {
		t.Fatalf("expected occupants %v, got %v", wantOccupants, got)
	}
	return yeErr
}

// TestCardinalityOneConflictDoesNotRetireUnrelatedDecision pins the incident the
// conflict guard exists for. Two unrelated decisions share one space + subject +
// predicate slot and differ only by object; a single-value assertion must refuse
// that slot rather than retiring the occupant it never named. The rejected
// assertion must leave no trace, and the same write as an additive fact must
// leave both decisions active.
func TestCardinalityOneConflictDoesNotRetireUnrelatedDecision(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "unrelated decision", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	project, err := eng.UpsertEntity(ctx, EntityInput{ID: "project:conflict", Type: "Project", CanonicalName: "Conflict"})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	decisionA, err := eng.UpsertEntity(ctx, EntityInput{ID: "decision:conflict-a", Type: "Decision", CanonicalName: "Adopt A"})
	if err != nil {
		t.Fatalf("upsert decision A: %v", err)
	}
	decisionB, err := eng.UpsertEntity(ctx, EntityInput{ID: "decision:conflict-b", Type: "Decision", CanonicalName: "Adopt B"})
	if err != nil {
		t.Fatalf("upsert decision B: %v", err)
	}
	first, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:conflict-a",
		Predicate:            "ADOPTED",
		SubjectID:            project.ID,
		ObjectID:             decisionA.ID,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert first decision: %v", err)
	}

	rawEng.mu.RLock()
	factsBefore := len(rawEng.facts)
	revisionsBefore := len(rawEng.factRevisions)
	rawEng.mu.RUnlock()

	_, err = eng.AssertFact(ctx, FactInput{
		ID:                   "fact:conflict-b",
		Predicate:            "ADOPTED",
		SubjectID:            project.ID,
		ObjectID:             decisionB.ID,
		Cardinality:          factCardinalityOne,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	requireFactConflict(t, err, []string{first.ID})

	rawEng.mu.RLock()
	factsAfter := len(rawEng.facts)
	revisionsAfter := len(rawEng.factRevisions)
	rawEng.mu.RUnlock()
	if factsBefore != factsAfter || revisionsBefore != revisionsAfter {
		t.Fatalf("expected the rejected conflict to leave state untouched, got facts %d->%d revisions %d->%d", factsBefore, factsAfter, revisionsBefore, revisionsAfter)
	}
	storedFirst, err := eng.GetFact(ctx, first.ID)
	if err != nil {
		t.Fatalf("get first decision: %v", err)
	}
	if storedFirst.Status != factStatusActive {
		t.Fatalf("expected the occupant to stay active, got %q", storedFirst.Status)
	}

	second, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact:conflict-b",
		Predicate:            "ADOPTED",
		SubjectID:            project.ID,
		ObjectID:             decisionB.ID,
		Cardinality:          factCardinalityMany,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert additive second decision: %v", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		got, err := eng.GetFact(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status != factStatusActive {
			t.Fatalf("expected %s active, got %q", id, got.Status)
		}
	}
}

// TestCardinalityOneConflictReportsOccupants pins the conflict details. Every
// overlapping occupant is reported, in sorted order, so the list is stable no
// matter which order the fact map was scanned in.
func TestCardinalityOneConflictReportsOccupants(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "occupants", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:occupants", Type: "Thing", CanonicalName: "Occupants"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	// Deliberately created out of order so a sorted report is a real assertion.
	occupants := []string{"fact:occupant-c", "fact:occupant-a", "fact:occupant-b"}
	for _, id := range occupants {
		if _, err := eng.AssertFact(ctx, FactInput{ID: id, Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: id, SupportingEpisodeIDs: []string{episode.EpisodeID}}); err != nil {
			t.Fatalf("assert %s: %v", id, err)
		}
	}
	want := append([]string(nil), occupants...)
	slices.Sort(want)

	_, err = eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "single", Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	requireFactConflict(t, err, want)

	// A fact in a different predicate slot is not an occupant of this one.
	if _, err := eng.AssertFact(ctx, FactInput{ID: "fact:occupant-other", Predicate: "HAS_OTHER_STATE", SubjectID: entity.ID, ValueText: "other", SupportingEpisodeIDs: []string{episode.EpisodeID}}); err != nil {
		t.Fatalf("assert other slot: %v", err)
	}
	_, err = eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "single", Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	requireFactConflict(t, err, want)
}

// TestCardinalityOneConflictHasZeroMutation pins that the guard runs before any
// identifier is allocated or revision appended: a rejected conflict must not
// consume the ID sequence or add a fact revision.
func TestCardinalityOneConflictHasZeroMutation(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	rawEng := eng.(*engine)
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "zero mutation", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:zero-mutation", Type: "Thing", CanonicalName: "Zero Mutation"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	occupant, err := eng.AssertFact(ctx, FactInput{ID: "fact:zero-mutation-occupant", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "occupant", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert occupant: %v", err)
	}

	rawEng.mu.RLock()
	sequenceBefore := rawEng.sequence
	revisionsBefore := len(rawEng.factRevisions)
	factsBefore := len(rawEng.facts)
	rawEng.mu.RUnlock()

	// No explicit ID: a guard that allocated the fact ID first would still burn a
	// sequence number even though nothing was inserted.
	_, err = eng.AssertFact(ctx, FactInput{Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "rejected", Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	requireFactConflict(t, err, []string{occupant.ID})

	rawEng.mu.RLock()
	defer rawEng.mu.RUnlock()
	if rawEng.sequence != sequenceBefore {
		t.Fatalf("expected the ID sequence to be untouched, got %d want %d", rawEng.sequence, sequenceBefore)
	}
	if len(rawEng.factRevisions) != revisionsBefore {
		t.Fatalf("expected no revision to be appended, got %d want %d", len(rawEng.factRevisions), revisionsBefore)
	}
	if len(rawEng.facts) != factsBefore {
		t.Fatalf("expected no fact to be inserted, got %d want %d", len(rawEng.facts), factsBefore)
	}
}

// TestCardinalityOneNonOverlappingAssertAllowed pins that the guard is bounded
// by validity overlap: a non-overlapping single-value assertion in an otherwise
// occupied slot is accepted and retires nothing.
func TestCardinalityOneNonOverlappingAssertAllowed(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "non-overlapping", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:non-overlapping", Type: "Thing", CanonicalName: "Non Overlapping"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	june := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	may := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	// The bounded fact ends exactly where the open-ended occupant begins, so the
	// two intervals do not overlap.
	boundedTo := june
	current, err := eng.AssertFact(ctx, FactInput{ID: "fact:non-overlap-current", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "current", ValidFrom: june, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert current: %v", err)
	}
	bounded, err := eng.AssertFact(ctx, FactInput{ID: "fact:non-overlap-bounded", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "bounded", ValidFrom: may, ValidTo: boundedTo, Cardinality: factCardinalityOne, SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert non-overlapping bounded fact: %v", err)
	}
	for _, id := range []string{current.ID, bounded.ID} {
		got, err := eng.GetFact(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status != factStatusActive {
			t.Fatalf("expected %s active, got %q", id, got.Status)
		}
	}
}

// TestAssertNeverRetires pins that an ordinary additive assertion is never a
// retirement: an earlier active fact in the same slot stays active.
func TestAssertNeverRetires(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "never retires", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "entity:never-retires", Type: "Thing", CanonicalName: "Never Retires"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	first, err := eng.AssertFact(ctx, FactInput{ID: "fact:never-retires-first", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "first", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert first: %v", err)
	}
	second, err := eng.AssertFact(ctx, FactInput{ID: "fact:never-retires-second", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "second", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert second: %v", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		got, err := eng.GetFact(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status != factStatusActive {
			t.Fatalf("expected %s active, got %q", id, got.Status)
		}
		if _, ok := got.Metadata["superseded_by"]; ok {
			t.Fatalf("expected no supersession on %s, got %#v", id, got.Metadata)
		}
	}
}
