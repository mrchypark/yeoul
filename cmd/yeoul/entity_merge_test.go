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

func TestCLIEntityMergeRejectsAlreadyDuplicateTarget(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-duplicate-target.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-duplicate-target.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-merge-duplicate",
      "kind":"note",
      "content":"duplicate target entities",
      "source":{"kind":"note","external_ref":"thread-merge-duplicate"}
    }
  ],
  "entities": [
    {"id":"project:yeoul-a","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:yeoul-b","type":"Project","canonical_name":"Yeoul"}
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
	runCLIExpectErr := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul-a", "--source", "project:yeoul-b", "--reason", "exact duplicate")

	beforeA := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-a")
	beforeB := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-b")
	if !strings.Contains(beforeB, `"duplicate_of": "project:yeoul-a"`) {
		t.Fatalf("expected first merge to mark source as duplicate, got %q", beforeB)
	}

	err := runCLIExpectErr("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul-b", "--source", "project:yeoul-a", "--reason", "merge duplicate target")
	if err == nil {
		t.Fatalf("expected merge of already-duplicate target to fail")
	}
	if !strings.Contains(err.Error(), "must be a canonical entity (not a duplicate)") {
		t.Fatalf("expected canonical-entity error, got %q", err.Error())
	}

	afterA := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-a")
	afterB := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-b")
	if afterA != beforeA {
		t.Fatalf("rejected merge changed target entity:\nbefore=%s\nafter=%s", beforeA, afterA)
	}
	if afterB != beforeB {
		t.Fatalf("rejected merge changed source entity:\nbefore=%s\nafter=%s", beforeB, afterB)
	}
	if strings.Contains(afterA, `"duplicate_of"`) {
		t.Fatalf("rejected merge marked source as a duplicate, got %q", afterA)
	}
	if !strings.Contains(afterB, `"duplicate_of": "project:yeoul-a"`) {
		t.Fatalf("source no longer keeps duplicate marker from first merge, got %q", afterB)
	}
}

func TestCLIEntityMergeRejectsAlreadyMarkedSourceWithoutMutation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-marked-source.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-marked-source.json")
	payload := `{"entities":[
  {"id":"project:a","type":"Project","canonical_name":"Yeoul"},
  {"id":"project:b","type":"Project","canonical_name":"Yeoul"},
  {"id":"project:c","type":"Project","canonical_name":"Yeoul"}
]}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}
	runCLI := func(args ...string) (string, error) {
		var stdout, stderr strings.Builder
		err := run(ctx, args, &stdout, &stderr)
		return stdout.String(), err
	}
	if _, err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if _, err := runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:a", "--source", "project:b", "--reason", "first merge"); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	beforeB, _ := runCLI("entity", "get", "--db", dbPath, "--id", "project:b")
	beforeC, _ := runCLI("entity", "get", "--db", dbPath, "--id", "project:c")
	_, err := runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:c", "--source", "project:b", "--reason", "redirect overwrite")
	if err == nil || !strings.Contains(err.Error(), "already a duplicate") {
		t.Fatalf("expected marked-source rejection, got %v", err)
	}
	afterB, _ := runCLI("entity", "get", "--db", dbPath, "--id", "project:b")
	afterC, _ := runCLI("entity", "get", "--db", dbPath, "--id", "project:c")
	if beforeB != afterB || beforeC != afterC {
		t.Fatalf("rejected redirect overwrite mutated entities:\nbefore=%s%safter=%s%s", beforeB, beforeC, afterB, afterC)
	}
}

func TestCLIEntityMergeRejectsIncompatibleScope(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-incompatible-scope.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-incompatible-scope.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-merge-scope",
      "kind":"note",
      "content":"incompatible merge scope",
      "source":{"kind":"note","external_ref":"thread-merge-scope"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
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
	runCLIExpectErr := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	beforeTarget := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul")
	beforeSource := runCLI("entity", "get", "--db", dbPath, "--id", "database:ladybug")

	err := runCLIExpectErr("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul", "--source", "database:ladybug", "--reason", "wrong scope")
	if err == nil {
		t.Fatalf("expected incompatible merge to fail")
	}
	if !strings.Contains(err.Error(), "database:ladybug") || !strings.Contains(err.Error(), "type") {
		t.Fatalf("expected error to name mismatched source and field, got %q", err.Error())
	}

	afterTarget := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul")
	afterSource := runCLI("entity", "get", "--db", dbPath, "--id", "database:ladybug")
	if afterTarget != beforeTarget {
		t.Fatalf("rejected merge changed target entity:\nbefore=%s\nafter=%s", beforeTarget, afterTarget)
	}
	if afterSource != beforeSource {
		t.Fatalf("rejected merge changed source entity:\nbefore=%s\nafter=%s", beforeSource, afterSource)
	}
	if strings.Contains(afterSource, `"duplicate_of"`) {
		t.Fatalf("rejected merge marked source as a duplicate, got %q", afterSource)
	}
}

func TestCLIEntityMergeRejectsConflictingStableKeysWithoutMutation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-stable-key-conflict.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-stable-key-conflict.json")
	payload := `{"entities":[
  {"id":"person:alpha","type":"Person","canonical_name":"Alex","stable_key":"alpha"},
  {"id":"person:beta","type":"Person","canonical_name":"Alex","stable_key":"beta"}
]}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}
	runCLI := func(args ...string) (string, error) {
		var stdout, stderr strings.Builder
		err := run(ctx, args, &stdout, &stderr)
		return stdout.String(), err
	}
	if _, err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	beforeTarget, _ := runCLI("entity", "get", "--db", dbPath, "--id", "person:alpha")
	beforeSource, _ := runCLI("entity", "get", "--db", dbPath, "--id", "person:beta")
	_, err := runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "person:alpha", "--source", "person:beta", "--reason", "conflicting keys")
	if err == nil || !strings.Contains(err.Error(), "stable_key") {
		t.Fatalf("expected stable-key conflict, got %v", err)
	}
	afterTarget, _ := runCLI("entity", "get", "--db", dbPath, "--id", "person:alpha")
	afterSource, _ := runCLI("entity", "get", "--db", dbPath, "--id", "person:beta")
	if beforeTarget != afterTarget || beforeSource != afterSource {
		t.Fatalf("rejected stable-key merge mutated entities:\nbefore=%s%safter=%s%s", beforeTarget, beforeSource, afterTarget, afterSource)
	}
}

func TestCLIEntityMergeMissingSourceLeavesStateUnchanged(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-missing-source.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-missing-source.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-merge-missing",
      "kind":"note",
      "content":"missing merge source",
      "source":{"kind":"note","external_ref":"thread-merge-missing"}
    }
  ],
  "entities": [
    {"id":"project:yeoul-a","type":"Project","canonical_name":"Yeoul","aliases":["Yeoul Alpha"]},
    {"id":"project:yeoul-b","type":"Project","canonical_name":"Yeoul Beta","aliases":["Yeoul Bee"]}
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
	runCLIExpectErr := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	beforeA := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-a")
	beforeB := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-b")

	err := runCLIExpectErr("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul-a", "--source", "project:yeoul-b,project:missing", "--reason", "missing source")
	if err == nil {
		t.Fatal("expected merge with a missing final source to fail")
	}

	afterA := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-a")
	afterB := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-b")
	if afterA != beforeA {
		t.Fatalf("failed merge changed target entity:\nbefore=%s\nafter=%s", beforeA, afterA)
	}
	if afterB != beforeB {
		t.Fatalf("failed merge changed the valid source entity:\nbefore=%s\nafter=%s", beforeB, afterB)
	}
	if strings.Contains(afterB, `"duplicate_of"`) {
		t.Fatalf("failed merge marked the valid source as a duplicate, got %q", afterB)
	}
	if strings.Contains(afterA, `"merged_from"`) {
		t.Fatalf("failed merge recorded merged_from on the target, got %q", afterA)
	}
}

// failBatchEngine delegates reads and single-entity writes to a real engine but
// fails the transactional batch write, simulating a storage failure during the
// merge commit.
type failBatchEngine struct {
	yeoul.Engine
	err         error
	upsertCalls int
}

func (e *failBatchEngine) UpsertEntity(ctx context.Context, input yeoul.EntityInput) (*yeoul.Entity, error) {
	e.upsertCalls++
	return e.Engine.UpsertEntity(ctx, input)
}

func (e *failBatchEngine) IngestBatch(context.Context, yeoul.BatchInput) (*yeoul.BatchResult, error) {
	return nil, e.err
}

func TestEntityMergeStorageFailureLeavesStateUnchanged(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-storage-failure.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-storage-failure.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-merge-storage",
      "kind":"note",
      "content":"merge storage failure",
      "source":{"kind":"note","external_ref":"thread-merge-storage"}
    }
  ],
  "entities": [
    {"id":"project:yeoul-a","type":"Project","canonical_name":"Yeoul","aliases":["Yeoul Alpha"]},
    {"id":"project:yeoul-b","type":"Project","canonical_name":"Yeoul Beta","aliases":["Yeoul Bee"]},
    {"id":"project:yeoul-c","type":"Project","canonical_name":"Yeoul Gamma","aliases":["Yeoul Cee"]}
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

	eng, err := yeoul.Open(ctx, yeoul.Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()

	before, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot before merge: %v", err)
	}

	injectedErr := errors.New("injected storage failure")
	injected := &failBatchEngine{Engine: eng, err: injectedErr}
	if _, _, err := mergeEntities(ctx, injected, "project:yeoul-a", []string{"project:yeoul-b", "project:yeoul-c"}, "injected failure"); !errors.Is(err, injectedErr) {
		t.Fatalf("expected injected storage failure, got %v", err)
	}
	if injected.upsertCalls != 0 {
		t.Fatalf("merge wrote %d entities outside the batch transaction", injected.upsertCalls)
	}

	after, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot after merge: %v", err)
	}

	if !reflect.DeepEqual(before.Entities, after.Entities) {
		t.Fatalf("failed merge changed entities:\nbefore=%#v\nafter=%#v", before.Entities, after.Entities)
	}
	if !reflect.DeepEqual(before.EntityRevisions, after.EntityRevisions) {
		t.Fatalf("failed merge changed entity revisions:\nbefore=%#v\nafter=%#v", before.EntityRevisions, after.EntityRevisions)
	}
	if before.Sequence != after.Sequence {
		t.Fatalf("failed merge advanced the database sequence from %d to %d", before.Sequence, after.Sequence)
	}
}
