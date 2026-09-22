package yeoul

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	latticedb "github.com/mrchypark/latticedb-go"
)

type noFactCandidatesStore struct{ stateStore }

type failingFactCandidatesStore struct{ stateStore }

func (failingFactCandidatesStore) FactCandidates(context.Context, []string, []string) ([]string, error) {
	return nil, errors.New("candidate storage failure")
}

func TestLatticeLookupFactsIndexedPathMatchesLegacyScan(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/lookup.db"
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open lattice engine: %v", err)
	}
	raw := eng.(*engine)
	defer eng.Close(ctx)

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "lookup", Content: "lookup differential", GroupID: "project:yeoul", Source: SourceInput{Kind: "test"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	left, err := eng.UpsertEntity(ctx, EntityInput{ID: "lookup:left", Type: "Project", CanonicalName: "left"})
	if err != nil {
		t.Fatalf("upsert left: %v", err)
	}
	right, err := eng.UpsertEntity(ctx, EntityInput{ID: "lookup:right", Type: "Service", CanonicalName: "right"})
	if err != nil {
		t.Fatalf("upsert right: %v", err)
	}
	alias, err := eng.UpsertEntity(ctx, EntityInput{ID: "lookup:left-alias", Type: "Project", CanonicalName: "left alias", Metadata: map[string]any{"duplicate_of": left.ID}})
	if err != nil {
		t.Fatalf("upsert alias: %v", err)
	}
	for _, input := range []FactInput{
		{ID: "lookup:1", Predicate: "USES", SubjectID: left.ID, ObjectID: right.ID, ValueText: "right-one", SupportingEpisodeIDs: []string{episode.EpisodeID}},
		{ID: "lookup:2", Predicate: "USES", SubjectID: left.ID, ObjectID: right.ID, ValueText: "right-two", SupportingEpisodeIDs: []string{episode.EpisodeID}},
		{ID: "lookup:3", Predicate: "OWNS", SubjectID: right.ID, ValueText: "other", SupportingEpisodeIDs: []string{episode.EpisodeID}},
		{ID: "lookup:4", Predicate: "USES", SubjectID: alias.ID, ObjectID: right.ID, ValueText: "alias", SupportingEpisodeIDs: []string{episode.EpisodeID}},
	} {
		if _, err := eng.AssertFact(ctx, input); err != nil {
			t.Fatalf("assert %s: %v", input.ID, err)
		}
	}
	if _, err := eng.RetractFact(ctx, "lookup:2", "differential"); err != nil {
		t.Fatalf("retract fact: %v", err)
	}

	requests := []FactLookupRequest{
		{SubjectIDs: []string{left.ID}, Page: Page{Limit: 1}},
		{ObjectIDs: []string{right.ID}, Scope: ScopeFilter{GroupIDs: []string{"project:yeoul"}}, Temporal: TemporalFilter{IncludeInactive: true}},
		{SubjectIDs: []string{left.ID}, ObjectIDs: []string{right.ID}, Predicates: []string{"USES"}, ObjectText: "right", Page: Page{Limit: 2}},
		{SubjectIDs: []string{left.ID}, Temporal: TemporalFilter{AsOf: ptrTime(time.Now().UTC().Add(time.Hour)), IncludeInactive: true}},
	}

	for _, req := range requests {
		indexed, err := eng.LookupFacts(ctx, req)
		if err != nil {
			t.Fatalf("indexed lookup %#v: %v", req, err)
		}
		original := raw.store
		raw.store = noFactCandidatesStore{stateStore: original}
		baseline, baselineErr := eng.LookupFacts(ctx, req)
		raw.store = original
		if baselineErr != nil {
			t.Fatalf("baseline lookup %#v: %v", req, baselineErr)
		}
		if !reflect.DeepEqual(indexed.Facts, baseline.Facts) || indexed.Meta.NextCursor != baseline.Meta.NextCursor {
			t.Fatalf("indexed result differs from baseline for %#v: indexed facts=%+v cursor=%q baseline facts=%+v cursor=%q", req, indexed.Facts, indexed.Meta.NextCursor, baseline.Facts, baseline.Meta.NextCursor)
		}
	}

	first, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{left.ID}, Page: Page{Limit: 1}})
	if err != nil || first.Meta.NextCursor == "" {
		t.Fatalf("first cursor page: response=%+v err=%v", first, err)
	}
	secondReq := FactLookupRequest{SubjectIDs: []string{left.ID}, Page: Page{Limit: 1, Cursor: first.Meta.NextCursor}}
	indexedSecond, err := eng.LookupFacts(ctx, secondReq)
	if err != nil {
		t.Fatalf("indexed second cursor page: %v", err)
	}
	original := raw.store
	raw.store = noFactCandidatesStore{stateStore: original}
	baselineSecond, err := eng.LookupFacts(ctx, secondReq)
	raw.store = original
	if err != nil || !reflect.DeepEqual(indexedSecond.Facts, baselineSecond.Facts) || indexedSecond.Meta.NextCursor != baselineSecond.Meta.NextCursor {
		t.Fatalf("cursor page differs from baseline: indexed=%+v baseline=%+v err=%v", indexedSecond, baselineSecond, err)
	}

	raw.store = failingFactCandidatesStore{stateStore: original}
	if _, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{left.ID}}); err == nil {
		t.Fatal("expected candidate storage failure to be returned")
	}
	raw.store = original
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := eng.LookupFacts(canceled, FactLookupRequest{SubjectIDs: []string{left.ID}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled indexed lookup error = %v, want context.Canceled", err)
	}

	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close before read-only reopen: %v", err)
	}
	// Remove the readiness marker to model an older LatticeDB. The read-only
	// reopen must use the full scan and must not recreate the marker.
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{})
	if err != nil {
		t.Fatalf("open marker test database: %v", err)
	}
	if err := db.Update(func(tx *latticedb.Tx) error {
		if err := tx.DeleteAppMetadata([]byte(latticeMetaFactEndpointEdges)); err != nil {
			return err
		}
		result, err := tx.Query("MATCH (f:Fact)-[:SUBJECT]->(e:Entity) WHERE f.id = $fact AND e.id = $entity RETURN id(f) AS fact_node, id(e) AS entity_node", map[string]latticedb.Value{"fact": "lookup:1", "entity": left.ID})
		if err != nil {
			return err
		}
		if len(result.Rows) != 1 {
			return errors.New("expected one subject edge to remove")
		}
		factNode, ok := result.Rows[0]["fact_node"].(int64)
		if !ok {
			return errors.New("malformed fact node id")
		}
		entityNode, ok := result.Rows[0]["entity_node"].(int64)
		if !ok {
			return errors.New("malformed entity node id")
		}
		return tx.DeleteEdge(uint64(factNode), uint64(entityNode), "SUBJECT")
	}); err != nil {
		t.Fatalf("remove readiness marker: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close marker test database: %v", err)
	}
	legacyWritable, err := Open(ctx, Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("reopen unready database writable: %v", err)
	}
	if _, err := legacyWritable.UpsertEntity(ctx, EntityInput{ID: "lookup:unrelated", Type: "Other", CanonicalName: "unrelated"}); err != nil {
		t.Fatalf("mutate unready database: %v", err)
	}
	if err := legacyWritable.Close(ctx); err != nil {
		t.Fatalf("close unready database: %v", err)
	}
	reopened, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only reopen: %v", err)
	}
	defer reopened.Close(ctx)
	previousExpansionObserver := factCandidateExpansionObserver
	expansions := 0
	factCandidateExpansionObserver = func() { expansions++ }
	readOnly, err := reopened.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{left.ID}})
	factCandidateExpansionObserver = previousExpansionObserver
	if err != nil || len(readOnly.Facts) != 2 {
		t.Fatalf("read-only indexed lookup: facts=%+v err=%v", readOnly.Facts, err)
	}
	if expansions != 0 {
		t.Fatalf("marker-absent read-only lookup expanded %d canonical entity sets, want 0", expansions)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close read-only reopen: %v", err)
	}
	db, err = latticedb.Open(dbPath, latticedb.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen marker verification database: %v", err)
	}
	defer db.Close()
	if err := db.View(func(tx *latticedb.Tx) error {
		if _, ok, err := tx.GetAppMetadata([]byte(latticeMetaFactEndpointEdges)); err != nil {
			return err
		} else if ok {
			return errors.New("read-only lookup recreated endpoint readiness marker")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func benchmarkLookupFactsFixture(b *testing.B, unrelatedEntities, facts int) (*engine, []string) {
	b.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: filepath.Join(b.TempDir(), "lookup.db"), CreateIfMissing: true})
	if err != nil {
		b.Fatalf("open lattice engine: %v", err)
	}
	b.Cleanup(func() { _ = eng.Close(ctx) })
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "benchmark", Content: "lookup benchmark"})
	if err != nil {
		b.Fatalf("ingest episode: %v", err)
	}
	target, err := eng.UpsertEntity(ctx, EntityInput{ID: "bench:target", Type: "Target", CanonicalName: "target"})
	if err != nil {
		b.Fatalf("upsert target: %v", err)
	}
	entities := make([]EntityInput, 0, unrelatedEntities)
	entityIDs := make([]string, 0, unrelatedEntities+1)
	for i := 0; i < unrelatedEntities; i++ {
		id := fmt.Sprintf("bench:entity:%04d", i)
		entities = append(entities, EntityInput{ID: id, Type: "Other", CanonicalName: fmt.Sprintf("other-%04d", i)})
		entityIDs = append(entityIDs, id)
	}
	factInputs := make([]FactInput, 0, facts)
	for i := 0; i < facts; i++ {
		subjectID := fmt.Sprintf("bench:entity:%04d", i%unrelatedEntities)
		if i%100 == 0 {
			subjectID = target.ID
		}
		factInputs = append(factInputs, FactInput{ID: fmt.Sprintf("bench:fact:%05d", i), Predicate: "BENCH", SubjectID: subjectID, ValueText: fmt.Sprintf("value-%d", i), SupportingEpisodeIDs: []string{episode.EpisodeID}})
	}
	if _, err := eng.IngestBatch(ctx, BatchInput{Entities: entities, Facts: factInputs}); err != nil {
		b.Fatalf("ingest benchmark batch: %v", err)
	}
	b.ReportMetric(float64(unrelatedEntities), "unrelated_entities")
	b.ReportMetric(float64(facts), "facts")
	entityIDs = append(entityIDs, "bench:target")
	return eng.(*engine), entityIDs
}

func BenchmarkLookupFactsCandidateReduction(b *testing.B) {
	for _, fixture := range []struct {
		entities int
		facts    int
	}{
		{entities: 16, facts: 2000},
		{entities: 140, facts: 300},
		{entities: 1024, facts: 2000},
	} {
		entityCount := fixture.entities
		e, entityIDs := benchmarkLookupFactsFixture(b, fixture.entities, fixture.facts)
		for _, query := range []struct {
			name string
			req  FactLookupRequest
		}{
			{name: "selective-subject", req: FactLookupRequest{SubjectIDs: []string{"bench:target"}}},
			{name: "broad-anchors", req: FactLookupRequest{SubjectIDs: entityIDs}},
		} {
			b.Run(fmt.Sprintf("entities=%d/%s/indexed", entityCount, query.name), func(b *testing.B) {
				benchmarkLookupFacts(b, e, query.req, false)
			})
			b.Run(fmt.Sprintf("entities=%d/%s/baseline", entityCount, query.name), func(b *testing.B) {
				benchmarkLookupFacts(b, e, query.req, true)
			})
		}
	}
}

func benchmarkLookupFacts(b *testing.B, e *engine, req FactLookupRequest, baseline bool) {
	ctx := context.Background()
	original := e.store
	if baseline {
		e.store = noFactCandidatesStore{stateStore: original}
	}
	b.Cleanup(func() { e.store = original })
	if _, err := e.LookupFacts(ctx, req); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.LookupFacts(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	e.store = original
}
