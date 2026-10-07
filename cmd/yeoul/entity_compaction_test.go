package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func TestBuildEntityMergeCandidatesRespectsStableKeys(t *testing.T) {
	entity := func(id, name string, metadata map[string]any) yeoul.EntityInput {
		return yeoul.EntityInput{ID: id, Type: "Person", CanonicalName: name, Metadata: metadata}
	}

	cases := []struct {
		name       string
		entities   []yeoul.EntityInput
		candidates int
	}{
		{
			name: "distinct stable keys stay distinct",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", map[string]any{"stable_key": "alpha"}),
				entity("person:b", "Alex", map[string]any{"stable_key": "beta"}),
			},
			candidates: 0,
		},
		{
			name: "matching stable keys are duplicates",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", map[string]any{"stable_key": "alpha"}),
				entity("person:b", "Alex", map[string]any{"stable_key": "alpha"}),
			},
			candidates: 1,
		},
		{
			name: "unkeyed same name is still a duplicate candidate",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", nil),
				entity("person:b", "Alex", nil),
			},
			candidates: 1,
		},
		{
			name: "keyed name is not an unkeyed display-name duplicate",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", map[string]any{"stable_key": "SAM"}),
				entity("person:b", "SAM", nil),
			},
			candidates: 0,
		},
		{
			name: "case-different names stay distinct",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", nil),
				entity("person:b", "alex", nil),
			},
			candidates: 0,
		},
		{
			name: "whitespace-distinct stable keys stay distinct",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", map[string]any{"stable_key": "key"}),
				entity("person:b", "Alex", map[string]any{"stable_key": " key "}),
			},
			candidates: 0,
		},
		{
			name: "non-string legacy stable keys are excluded",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", map[string]any{"stable_key": 123}),
				entity("person:b", "Alex", map[string]any{"stable_key": "123"}),
			},
			candidates: 0,
		},
		{
			name: "already marked duplicates are ignored",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", nil),
				entity("person:b", "Alex", map[string]any{"duplicate_of": "person:a"}),
			},
			candidates: 0,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidates := buildEntityMergeCandidates(&exportFile{Entities: testCase.entities})
			if len(candidates) != testCase.candidates {
				t.Fatalf("expected %d candidates, got %d (%#v)", testCase.candidates, len(candidates), candidates)
			}
		})
	}
}

func TestApplyEntityMergeCandidateBatchFailureLeavesGroupUnchanged(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "compact-batch-failure.ltdb")
	eng, err := yeoul.Open(ctx, yeoul.Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	if _, err := eng.IngestBatch(ctx, yeoul.BatchInput{Entities: []yeoul.EntityInput{
		{ID: "person:target", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"owner": "target"}},
		{ID: "person:source-a", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"owner": "source-a"}},
		{ID: "person:source-b", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"owner": "source-b"}},
	}}); err != nil {
		t.Fatalf("seed entities: %v", err)
	}
	before, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot before compaction: %v", err)
	}
	injectedErr := errors.New("injected batch failure")
	injected := &failBatchEngine{Engine: eng, err: injectedErr}
	candidate := entityMergeCandidate{TargetID: "person:target", SourceIDs: []string{"person:source-a", "person:source-b"}}
	marked, err := applyEntityMergeCandidate(ctx, injected, candidate)
	if !errors.Is(err, injectedErr) || marked != 0 {
		t.Fatalf("expected failed batch and zero marked sources, got marked=%d err=%v", marked, err)
	}
	if injected.batchCalls != 1 || injected.upsertCalls != 0 {
		t.Fatalf("expected one batch and no standalone upserts, got batches=%d upserts=%d", injected.batchCalls, injected.upsertCalls)
	}
	after, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot after compaction: %v", err)
	}
	if !reflect.DeepEqual(before.Entities, after.Entities) || !reflect.DeepEqual(before.EntityRevisions, after.EntityRevisions) || before.Sequence != after.Sequence {
		t.Fatalf("failed group update changed the snapshot:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestApplyEntityMergeCandidatePreservesCountAndMetadata(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "compact-batch-success.ltdb")
	eng, err := yeoul.Open(ctx, yeoul.Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	if _, err := eng.IngestBatch(ctx, yeoul.BatchInput{Entities: []yeoul.EntityInput{
		{ID: "person:target", Namespace: "people", Type: "Person", CanonicalName: "Alex", Aliases: []string{"Target Alias"}, Metadata: map[string]any{"owner": "target", "compaction_entity_duplicates": []string{"person:prior"}}},
		{ID: "person:source-a", Namespace: "legacy", Type: "Person", CanonicalName: "Alex", Aliases: []string{"Source Alias"}, Metadata: map[string]any{"owner": "source-a"}},
		{ID: "person:source-b", Namespace: "archive", Type: "Person", CanonicalName: "Alex", Metadata: map[string]any{"owner": "source-b"}},
	}}); err != nil {
		t.Fatalf("seed entities: %v", err)
	}
	candidate := entityMergeCandidate{TargetID: "person:target", SourceIDs: []string{"person:source-a", "person:source-b"}, DriftNamespace: true}
	marked, err := applyEntityMergeCandidate(ctx, eng, candidate)
	if err != nil || marked != len(candidate.SourceIDs) {
		t.Fatalf("expected %d marked sources, got %d: %v", len(candidate.SourceIDs), marked, err)
	}
	target, err := eng.GetEntity(ctx, candidate.TargetID)
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if target.Metadata["owner"] != "target" || !reflect.DeepEqual(anyStrings(target.Metadata["compaction_entity_duplicates"]), []string{"person:prior", "person:source-a", "person:source-b"}) {
		t.Fatalf("target metadata was not preserved and extended: %#v", target.Metadata)
	}
	markedAt, ok := target.Metadata["compaction_marked"].(string)
	if !ok || markedAt == "" || target.Metadata["merge_drift_namespace"] == nil {
		t.Fatalf("target compaction metadata missing: %#v", target.Metadata)
	}
	if !reflect.DeepEqual(target.Aliases, []string{"Target Alias"}) {
		t.Fatalf("target aliases changed: %#v", target.Aliases)
	}
	for _, id := range candidate.SourceIDs {
		source, err := eng.GetEntity(ctx, id)
		if err != nil {
			t.Fatalf("get source %s: %v", id, err)
		}
		if source.Metadata["owner"] != strings.TrimPrefix(id, "person:") || source.Metadata["duplicate_of"] != target.ID || source.Metadata["compaction_marked"] != markedAt || source.Metadata["merge_drift_namespace"] == nil {
			t.Fatalf("source compaction metadata missing or overwritten for %s: %#v", id, source.Metadata)
		}
		if id == "person:source-a" && !reflect.DeepEqual(source.Aliases, []string{"Source Alias"}) {
			t.Fatalf("source aliases changed: %#v", source.Aliases)
		}
	}
}

func TestCLIEntityCompactionKeepsDistinctStableKeys(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "entity-compaction.ltdb")
	ingestPath := filepath.Join(tmpDir, "entity-compaction.json")

	payload := `{
  "entities": [
    {"id":"person:alpha","type":"Person","canonical_name":"Alex","stable_key":"alpha-1"},
    {"id":"person:beta","type":"Person","canonical_name":"Alex","stable_key":"beta-1"},
    {"id":"person:gamma","type":"Person","canonical_name":"Alex","stable_key":"gamma-1"},
    {"id":"person:delta","type":"Person","canonical_name":"Alex","stable_key":"gamma-1"},
    {"id":"person:pad-a","type":"Person","canonical_name":"Sam","stable_key":"key"},
    {"id":"person:pad-b","type":"Person","canonical_name":"Sam","stable_key":" key "}
  ]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}

	runCLI := func(args ...string) string {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
		return stdout.String()
	}

	runCLI("init", "--db", dbPath)
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)

	dryRun := runCLI("admin", "compact", "--db", dbPath, "--json")
	if !strings.Contains(dryRun, `"entity_duplicate_candidates": 1`) {
		t.Fatalf("expected exactly one entity candidate, got %q", dryRun)
	}
	runCLI("admin", "compact", "--confirm", "--apply", "--db", dbPath)

	for _, id := range []string{"person:alpha", "person:beta"} {
		record := runCLI("entity", "get", "--db", dbPath, "--id", id)
		if strings.Contains(record, "duplicate_of") {
			t.Fatalf("expected %s to stay a distinct identity, got %q", id, record)
		}
	}

	marked := 0
	for _, id := range []string{"person:gamma", "person:delta"} {
		record := runCLI("entity", "get", "--db", dbPath, "--id", id)
		if strings.Contains(record, "duplicate_of") {
			marked++
		}
	}
	if marked != 1 {
		t.Fatalf("expected exactly one of the matching stable keys to be marked, got %d", marked)
	}

	for _, id := range []string{"person:pad-a", "person:pad-b"} {
		record := runCLI("entity", "get", "--db", dbPath, "--id", id)
		if strings.Contains(record, "duplicate_of") {
			t.Fatalf("expected %s to stay a distinct identity, got %q", id, record)
		}
	}
	search := runCLI("search", "--db", dbPath, "--query", "Sam", "--json")
	if !strings.Contains(search, "person:pad-a") || !strings.Contains(search, "person:pad-b") {
		t.Fatalf("expected both whitespace-distinct entities to stay searchable, got %q", search)
	}
}

// TestCLIAdminCompactApplyOwnsTheDatabase verifies that applying compaction is
// explicit maintenance: it takes writable ownership before it discovers its
// candidates, so it is refused while another store holds the database instead
// of applying a candidate list taken from state that store can still change.
// The read-only preview keeps working, because only the apply changes the
// database and it is the only step that needs the writable ownership.
func TestCLIAdminCompactApplyOwnsTheDatabase(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "compact-ownership.ltdb")
	ingestPath := filepath.Join(tmpDir, "compact-ownership.json")

	payload := `{
  "entities": [
    {"id":"person:alpha","type":"Person","canonical_name":"Alex","stable_key":"alex-1"},
    {"id":"person:beta","type":"Person","canonical_name":"Alex","stable_key":"alex-1"}
  ]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}

	runCLI := func(args ...string) (string, error) {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		err := run(ctx, args, &stdout, &stderr)
		return stdout.String(), err
	}
	if _, err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	owner, err := yeoul.Open(ctx, yeoul.Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open competing owner: %v", err)
	}

	if _, err := runCLI("admin", "compact", "--db", dbPath, "--json"); err != nil {
		t.Fatalf("expected the preview to keep working while another owner reads, got %v", err)
	}
	if _, err := runCLI("admin", "compact", "--confirm", "--apply", "--db", dbPath); err == nil {
		t.Fatal("expected compaction to be refused while another owner holds the database")
	} else if !strings.Contains(err.Error(), "database is owned by another process") {
		t.Fatalf("expected an ownership refusal, got %v", err)
	}

	if err := owner.Close(ctx); err != nil {
		t.Fatalf("close competing owner: %v", err)
	}
	if _, err := runCLI("admin", "compact", "--confirm", "--apply", "--db", dbPath); err != nil {
		t.Fatalf("expected compaction to apply once ownership is free, got %v", err)
	}
	record, err := runCLI("entity", "get", "--db", dbPath, "--id", "person:beta")
	if err != nil {
		t.Fatalf("get compacted entity: %v", err)
	}
	if !strings.Contains(record, "duplicate_of") {
		t.Fatalf("expected the duplicate to be marked after compaction, got %q", record)
	}
}
