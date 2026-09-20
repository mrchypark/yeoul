package yeoul

import (
	"context"
	"sync"
	"testing"
)

// metadataCase builds a fresh metadata map for one JSON-compatible container
// shape, mutates the container in place, and reports whether a value is still
// the original one. It lets every isolation path be checked against each
// accepted container type without duplicating the assertions.
type metadataCase struct {
	name   string
	build  func() map[string]any
	mutate func(map[string]any)
	intact func(map[string]any) bool
}

// isoTaggedMetadata and isoValuesMetadata are JSON-compatible structs holding a
// mutable container behind an exported, tagged field. Callers may pass values
// like these inside metadata, so the deep copy has to reach the nested map and
// slice rather than leaving them shared with caller memory.
type isoTaggedMetadata struct {
	Tags map[string]string `json:"tags"`
}

type isoValuesMetadata struct {
	Values []int `json:"values"`
}

func metadataIsolationCases() []metadataCase {
	return []metadataCase{
		{
			name:   "map_string_string",
			build:  func() map[string]any { return map[string]any{"tags": map[string]string{"k": "v"}} },
			mutate: func(m map[string]any) { m["tags"].(map[string]string)["k"] = "mutated" },
			intact: func(m map[string]any) bool { return m["tags"].(map[string]string)["k"] == "v" },
		},
		{
			name:   "slice_int",
			build:  func() map[string]any { return map[string]any{"nums": []int{1, 2, 3}} },
			mutate: func(m map[string]any) { m["nums"].([]int)[0] = 99 },
			intact: func(m map[string]any) bool { return m["nums"].([]int)[0] == 1 },
		},
		{
			name:   "slice_map_string_any",
			build:  func() map[string]any { return map[string]any{"items": []map[string]any{{"k": "v"}}} },
			mutate: func(m map[string]any) { m["items"].([]map[string]any)[0]["k"] = "mutated" },
			intact: func(m map[string]any) bool { return m["items"].([]map[string]any)[0]["k"] == "v" },
		},
		{
			name:   "map_string_int",
			build:  func() map[string]any { return map[string]any{"counts": map[string]int{"a": 1}} },
			mutate: func(m map[string]any) { m["counts"].(map[string]int)["a"] = 99 },
			intact: func(m map[string]any) bool { return m["counts"].(map[string]int)["a"] == 1 },
		},
		{
			name: "nested_map_combo",
			build: func() map[string]any {
				return map[string]any{"outer": map[string]any{"inner": map[string]string{"k": "v"}}}
			},
			mutate: func(m map[string]any) {
				m["outer"].(map[string]any)["inner"].(map[string]string)["k"] = "mutated"
			},
			intact: func(m map[string]any) bool {
				return m["outer"].(map[string]any)["inner"].(map[string]string)["k"] == "v"
			},
		},
		{
			name: "array_of_maps",
			build: func() map[string]any {
				return map[string]any{"pair": [2]map[string]any{{"k": "v"}, {"k": "v"}}}
			},
			mutate: func(m map[string]any) {
				arr := m["pair"].([2]map[string]any)
				arr[0]["k"] = "mutated"
			},
			intact: func(m map[string]any) bool { return m["pair"].([2]map[string]any)[0]["k"] == "v" },
		},
		{
			name: "slice_any_typed_map",
			build: func() map[string]any {
				return map[string]any{"mixed": []any{map[string]int{"a": 1}, map[string]string{"k": "v"}}}
			},
			mutate: func(m map[string]any) {
				mixed := m["mixed"].([]any)
				mixed[0].(map[string]int)["a"] = 99
				mixed[1].(map[string]string)["k"] = "mutated"
			},
			intact: func(m map[string]any) bool {
				mixed := m["mixed"].([]any)
				return mixed[0].(map[string]int)["a"] == 1 && mixed[1].(map[string]string)["k"] == "v"
			},
		},
		{
			name: "struct_map_string_string",
			build: func() map[string]any {
				return map[string]any{"obj": isoTaggedMetadata{Tags: map[string]string{"k": "v"}}}
			},
			mutate: func(m map[string]any) { m["obj"].(isoTaggedMetadata).Tags["k"] = "mutated" },
			intact: func(m map[string]any) bool { return m["obj"].(isoTaggedMetadata).Tags["k"] == "v" },
		},
		{
			name:   "struct_slice_int",
			build:  func() map[string]any { return map[string]any{"obj": isoValuesMetadata{Values: []int{1, 2, 3}}} },
			mutate: func(m map[string]any) { m["obj"].(isoValuesMetadata).Values[0] = 99 },
			intact: func(m map[string]any) bool { return m["obj"].(isoValuesMetadata).Values[0] == 1 },
		},
		{
			name: "pointer_to_slice_int",
			build: func() map[string]any {
				nums := []int{1, 2, 3}
				return map[string]any{"ptr": &nums}
			},
			mutate: func(m map[string]any) { (*m["ptr"].(*[]int))[0] = 99 },
			intact: func(m map[string]any) bool { return (*m["ptr"].(*[]int))[0] == 1 },
		},
		{
			name: "pointer_to_map_string_string",
			build: func() map[string]any {
				tags := map[string]string{"k": "v"}
				return map[string]any{"ptr": &tags}
			},
			mutate: func(m map[string]any) { (*m["ptr"].(*map[string]string))["k"] = "mutated" },
			intact: func(m map[string]any) bool { return (*m["ptr"].(*map[string]string))["k"] == "v" },
		},
		{
			name:   "nil_pointer_to_slice_int",
			build:  func() map[string]any { return map[string]any{"ptr": (*[]int)(nil)} },
			mutate: func(map[string]any) {},
			intact: func(m map[string]any) bool {
				ptr, ok := m["ptr"].(*[]int)
				return ok && ptr == nil
			},
		},
	}
}

// TestCloneAnyTerminatesOnCyclicContainers pins the visited-pointer guard: a
// metadata value that references itself must clone without infinite recursion,
// and the clone must reference itself rather than the caller's container.
func TestCloneAnyTerminatesOnCyclicContainers(t *testing.T) {
	src := map[string]any{}
	src["self"] = src
	src["nested"] = []any{src}

	clone := cloneAnyMap(src)

	self, ok := clone["self"].(map[string]any)
	if !ok {
		t.Fatalf("expected the cloned self-reference to stay a map, got %#v", clone["self"])
	}
	clone["probe"] = "from-clone"
	if self["probe"] != "from-clone" {
		t.Fatal("expected the cloned self-reference to point at the clone itself")
	}

	src["marker"] = "from-source"
	if _, leaked := clone["marker"]; leaked {
		t.Fatal("cloning a cyclic map aliased the caller container")
	}

	nested, ok := clone["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("expected a cloned nested slice, got %#v", clone["nested"])
	}
	inner, ok := nested[0].(map[string]any)
	if !ok || inner["probe"] != "from-clone" {
		t.Fatalf("expected the nested cycle to resolve to the clone, got %#v", nested[0])
	}
}

func openIsolationEngine(t *testing.T) (Engine, context.Context) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close(ctx) })
	return eng, ctx
}

func TestMetadataContainersIsolatedFromInput(t *testing.T) {
	for _, tc := range metadataIsolationCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, ctx := openIsolationEngine(t)
			input := tc.build()
			entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: tc.name, Metadata: input})
			if err != nil {
				t.Fatalf("upsert entity: %v", err)
			}
			tc.mutate(input)
			got, err := eng.GetEntity(ctx, entity.ID)
			if err != nil {
				t.Fatalf("get entity: %v", err)
			}
			if !tc.intact(got.Metadata) {
				t.Fatalf("input mutation leaked into engine state: %#v", got.Metadata)
			}
		})
	}
}

func TestMetadataContainersIsolatedFromResult(t *testing.T) {
	for _, tc := range metadataIsolationCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, ctx := openIsolationEngine(t)
			entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: tc.name, Metadata: tc.build()})
			if err != nil {
				t.Fatalf("upsert entity: %v", err)
			}
			tc.mutate(entity.Metadata)
			got, err := eng.GetEntity(ctx, entity.ID)
			if err != nil {
				t.Fatalf("get entity: %v", err)
			}
			if !tc.intact(got.Metadata) {
				t.Fatalf("result mutation leaked into engine state: %#v", got.Metadata)
			}
		})
	}
}

func TestMetadataContainersIsolatedFromSnapshot(t *testing.T) {
	for _, tc := range metadataIsolationCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, ctx := openIsolationEngine(t)
			entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: tc.name, Metadata: tc.build()})
			if err != nil {
				t.Fatalf("upsert entity: %v", err)
			}
			snapshot, err := Snapshot(ctx, eng)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			stored, ok := snapshot.Entities[entity.ID]
			if !ok {
				t.Fatalf("expected entity in snapshot: %#v", snapshot.Entities)
			}
			tc.mutate(stored.Metadata)
			got, err := eng.GetEntity(ctx, entity.ID)
			if err != nil {
				t.Fatalf("get entity: %v", err)
			}
			if !tc.intact(got.Metadata) {
				t.Fatalf("snapshot mutation leaked into engine state: %#v", got.Metadata)
			}
		})
	}
}

func TestMetadataContainersIsolatedFromEntityHistory(t *testing.T) {
	for _, tc := range metadataIsolationCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, ctx := openIsolationEngine(t)
			entity, err := eng.UpsertEntity(ctx, EntityInput{
				Type:          "Thing",
				CanonicalName: "before " + tc.name,
				StableKey:     "iso-" + tc.name,
				Metadata:      tc.build(),
			})
			if err != nil {
				t.Fatalf("upsert entity: %v", err)
			}
			if _, err := eng.UpsertEntity(ctx, EntityInput{
				Type:          "Thing",
				CanonicalName: "after " + tc.name,
				StableKey:     "iso-" + tc.name,
			}); err != nil {
				t.Fatalf("update entity: %v", err)
			}
			rawEng := eng.(*engine)
			rawEng.mu.RLock()
			stored := rawEng.entities[entity.ID]
			rawEng.mu.RUnlock()
			snapshots := entityHistorySnapshots(stored.Metadata)
			if len(snapshots) == 0 {
				t.Fatalf("expected an entity history snapshot, got %#v", stored.Metadata)
			}
			if !tc.intact(snapshots[0].Metadata) {
				t.Fatalf("history snapshot lost the original metadata: %#v", snapshots[0].Metadata)
			}
			tc.mutate(snapshots[0].Metadata)
			rawEng.mu.RLock()
			storedAgain := rawEng.entities[entity.ID]
			rawEng.mu.RUnlock()
			if !tc.intact(entityHistorySnapshots(storedAgain.Metadata)[0].Metadata) {
				t.Fatalf("history mutation leaked into engine state: %#v", storedAgain.Metadata)
			}
		})
	}
}

func TestFactAndEpisodeMetadataContainersIsolated(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	episodeMetadata := map[string]any{"nums": []int{1, 2, 3}}
	sourceMetadata := map[string]any{"tags": map[string]string{"k": "v"}}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:     "note",
		Content:  "isolation note",
		Source:   SourceInput{Kind: "note", ExternalRef: "isolation", Metadata: sourceMetadata},
		Metadata: episodeMetadata,
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "isolation"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	factMetadata := map[string]any{"items": []map[string]any{{"k": "v"}}}
	fact, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_META",
		SubjectID:            entity.ID,
		ValueText:            "isolation",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Metadata:             factMetadata,
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	episodeMetadata["nums"].([]int)[0] = 99
	sourceMetadata["tags"].(map[string]string)["k"] = "mutated"
	factMetadata["items"].([]map[string]any)[0]["k"] = "mutated"

	gotEpisode, err := eng.GetEpisode(ctx, episode.EpisodeID)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	if gotEpisode.Metadata["nums"].([]int)[0] != 1 {
		t.Fatalf("episode input mutation leaked: %#v", gotEpisode.Metadata)
	}
	gotSource, err := eng.GetSource(ctx, episode.SourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if gotSource.Metadata["tags"].(map[string]string)["k"] != "v" {
		t.Fatalf("source input mutation leaked: %#v", gotSource.Metadata)
	}
	gotFact, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if gotFact.Metadata["items"].([]map[string]any)[0]["k"] != "v" {
		t.Fatalf("fact input mutation leaked: %#v", gotFact.Metadata)
	}

	gotEpisode.Metadata["nums"].([]int)[0] = 7
	gotSource.Metadata["tags"].(map[string]string)["k"] = "result"
	gotFact.Metadata["items"].([]map[string]any)[0]["k"] = "result"

	againEpisode, err := eng.GetEpisode(ctx, episode.EpisodeID)
	if err != nil {
		t.Fatalf("get episode again: %v", err)
	}
	if againEpisode.Metadata["nums"].([]int)[0] != 1 {
		t.Fatalf("episode result mutation leaked: %#v", againEpisode.Metadata)
	}
	againSource, err := eng.GetSource(ctx, episode.SourceID)
	if err != nil {
		t.Fatalf("get source again: %v", err)
	}
	if againSource.Metadata["tags"].(map[string]string)["k"] != "v" {
		t.Fatalf("source result mutation leaked: %#v", againSource.Metadata)
	}
	againFact, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact again: %v", err)
	}
	if againFact.Metadata["items"].([]map[string]any)[0]["k"] != "v" {
		t.Fatalf("fact result mutation leaked: %#v", againFact.Metadata)
	}
}

func TestIncludedRecordsDoNotShareEngineMetadata(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:     "note",
		Content:  "isolation note",
		Source:   SourceInput{Kind: "note", ExternalRef: "isolation", Metadata: map[string]any{"tags": map[string]string{"k": "v"}}},
		Metadata: map[string]any{"nums": []int{1, 2, 3}},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "isolation", Metadata: map[string]any{"counts": map[string]int{"a": 1}}})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_META",
		SubjectID:            entity.ID,
		ValueText:            "isolation",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Metadata:             map[string]any{"items": []map[string]any{{"k": "v"}}},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	assertEngineIntact := func(t *testing.T) {
		t.Helper()
		gotEpisode, err := eng.GetEpisode(ctx, episode.EpisodeID)
		if err != nil {
			t.Fatalf("get episode: %v", err)
		}
		if gotEpisode.Metadata["nums"].([]int)[0] != 1 {
			t.Fatalf("included episode shared engine metadata: %#v", gotEpisode.Metadata)
		}
		gotSource, err := eng.GetSource(ctx, episode.SourceID)
		if err != nil {
			t.Fatalf("get source: %v", err)
		}
		if gotSource.Metadata["tags"].(map[string]string)["k"] != "v" {
			t.Fatalf("included source shared engine metadata: %#v", gotSource.Metadata)
		}
		gotEntity, err := eng.GetEntity(ctx, entity.ID)
		if err != nil {
			t.Fatalf("get entity: %v", err)
		}
		if gotEntity.Metadata["counts"].(map[string]int)["a"] != 1 {
			t.Fatalf("included entity shared engine metadata: %#v", gotEntity.Metadata)
		}
		gotFact, err := eng.GetFact(ctx, fact.ID)
		if err != nil {
			t.Fatalf("get fact: %v", err)
		}
		if gotFact.Metadata["items"].([]map[string]any)[0]["k"] != "v" {
			t.Fatalf("included fact shared engine metadata: %#v", gotFact.Metadata)
		}
	}

	lookup, err := eng.LookupFacts(ctx, FactLookupRequest{
		Meta:    QueryMeta{SpaceID: "default"},
		Include: Include{SupportingFacts: true, SupportingEpisodes: true, RelatedEntities: true},
	})
	if err != nil {
		t.Fatalf("lookup facts: %v", err)
	}
	if len(lookup.Included.Episodes) == 0 || len(lookup.Included.Sources) == 0 || len(lookup.Included.Entities) == 0 || len(lookup.Included.Facts) == 0 {
		t.Fatalf("expected included records from lookup, got %#v", lookup.Included)
	}
	lookup.Included.Episodes[0].Metadata["nums"].([]int)[0] = 99
	lookup.Included.Sources[0].Metadata["tags"].(map[string]string)["k"] = "mutated"
	lookup.Included.Entities[0].Metadata["counts"].(map[string]int)["a"] = 99
	lookup.Included.Facts[0].Metadata["items"].([]map[string]any)[0]["k"] = "mutated"
	assertEngineIntact(t)

	search, err := eng.Search(ctx, SearchRequest{
		QueryText: "isolation",
		Types:     []string{"episode"},
		Include:   Include{SupportingEpisodes: true},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(search.Included.Episodes) == 0 || len(search.Included.Sources) == 0 {
		t.Fatalf("expected included episode and source from search, got %#v", search.Included)
	}
	search.Included.Episodes[0].Metadata["nums"].([]int)[0] = 99
	search.Included.Sources[0].Metadata["tags"].(map[string]string)["k"] = "mutated"
	assertEngineIntact(t)
}

func TestMetadataIsolationUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	entity, err := eng.UpsertEntity(ctx, EntityInput{
		Type:          "Thing",
		CanonicalName: "concurrent",
		Metadata: map[string]any{
			"tags": map[string]string{"k": "v"},
			"nums": []int{1, 2, 3},
		},
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	var wg sync.WaitGroup
	for writer := 0; writer < 8; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, err := eng.GetEntity(ctx, entity.ID)
				if err != nil {
					t.Errorf("get entity: %v", err)
					return
				}
				got.Metadata["tags"].(map[string]string)["k"] = "mutated"
				got.Metadata["nums"].([]int)[0] = 99
			}
		}()
	}
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, err := eng.GetEntity(ctx, entity.ID)
				if err != nil {
					t.Errorf("get entity: %v", err)
					return
				}
				if got.Metadata["tags"].(map[string]string)["k"] != "v" || got.Metadata["nums"].([]int)[0] != 1 {
					t.Errorf("observed shared metadata mutation: %#v", got.Metadata)
					return
				}
			}
		}()
	}
	wg.Wait()
}
