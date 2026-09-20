package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
