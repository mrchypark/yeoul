package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

// TestCLIEntityNearDuplicateGuard reproduces issue #139: a canonical entity
// exists under one namespace, and an automatic upsert that drifts the namespace
// (here a project-scoped namespace against a blank stored one) derives a fresh
// ID. The assert must fail closed instead of creating a second entity, and the
// resolver must still find the canonical entity from the drifted tuple.
//
// The seeded entity carries the lowercase display name as an alias because the
// resolver's display-name rule is exact: the recorded drift form the docs name
// is a canonical name matching an alias ("yeoul" vs "Yeoul"), not a folded
// comparison of two canonical names.
func TestCLIEntityNearDuplicateGuard(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "near-duplicate.ltdb")
	ingestPath := filepath.Join(tmpDir, "near-duplicate.json")

	payload := `{
  "episodes": [
    {"id":"ep-139","kind":"note","content":"issue 139 identity drift","source":{"kind":"note","external_ref":"thread-139"}}
  ],
  "entities": [
    {"id":"project:yeoul","namespace":"","type":"Project","canonical_name":"Yeoul","aliases":["yeoul"]}
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
		t.Fatalf("ingest json: %v", err)
	}

	_, err := runCLI(
		"fact", "assert",
		"--db", dbPath,
		"--predicate", "USES_STORAGE_ENGINE",
		"--upsert-subject",
		"--subject-namespace", "project:yeoul",
		"--subject-type", "project",
		"--subject-name", "yeoul",
		"--cardinality", "many",
		"--supporting-episodes", "ep-139",
		"--json",
	)
	if err == nil {
		t.Fatal("expected the drifted automatic upsert to fail closed")
	}
	var apiErr *yeoul.Error
	if !errors.As(err, &apiErr) || apiErr.Code != yeoul.ErrEntityNearDuplicate {
		t.Fatalf("expected %s, got %v", yeoul.ErrEntityNearDuplicate, err)
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	for _, field := range []string{"role", "derived_id", "existing_ids", "namespace", "type", "canonical_name"} {
		if _, ok := apiErr.Details[field]; !ok {
			t.Fatalf("expected details to carry %q, got %v", field, apiErr.Details)
		}
	}

	// The guard must not have created the drifted entity.
	derivedID := yeoul.EntityID("project:yeoul", "project", "yeoul")
	if _, err := runCLI("entity", "get", "--db", dbPath, "--id", derivedID); err == nil {
		t.Fatalf("expected no entity to be created at the derived id %q", derivedID)
	}

	// The resolver finds the canonical entity from the drifted tuple.
	resolved, err := runCLI("entity", "resolve", "--db", dbPath, "--type", "Project", "--name", "Yeoul", "--json")
	if err != nil {
		t.Fatalf("entity resolve: %v", err)
	}
	if !strings.Contains(resolved, "project:yeoul") {
		t.Fatalf("expected the canonical entity from resolve, got %q", resolved)
	}
}

// TestCLIEntityResolveReportsDrift exercises the text and JSON output of the
// resolver, including the drift flag for a folded namespace.
func TestCLIEntityResolveReportsDrift(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "resolve-drift.ltdb")
	ingestPath := filepath.Join(tmpDir, "resolve-drift.json")

	payload := `{
  "entities": [
    {"id":"repo:yeoul","namespace":"Repo","type":"Project","canonical_name":"Yeoul"}
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
		t.Fatalf("ingest json: %v", err)
	}

	text, err := runCLI("entity", "resolve", "--db", dbPath, "--type", "project", "--name", "Yeoul", "--namespace", "repo")
	if err != nil {
		t.Fatalf("entity resolve: %v", err)
	}
	if !strings.Contains(text, "match=repo:yeoul") || !strings.Contains(text, "drifted=true") {
		t.Fatalf("expected a drifted match line, got %q", text)
	}

	jsonOut, err := runCLI("entity", "resolve", "--db", dbPath, "--type", "project", "--name", "Yeoul", "--namespace", "repo", "--json")
	if err != nil {
		t.Fatalf("entity resolve json: %v", err)
	}
	if !strings.Contains(jsonOut, `"matches"`) || !strings.Contains(jsonOut, `"drifted"`) {
		t.Fatalf("expected matches and drifted in JSON, got %q", jsonOut)
	}

	// A missing identity keeps the engine's not-found code for exit 3.
	_, err = runCLI("entity", "resolve", "--db", dbPath, "--type", "Project", "--name", "Missing")
	var apiErr *yeoul.Error
	if !errors.As(err, &apiErr) || apiErr.Code != yeoul.ErrEntityNotFound {
		t.Fatalf("expected %s, got %v", yeoul.ErrEntityNotFound, err)
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
}

// TestCLIEntityMergeReconcilesDrift verifies that a namespace-drift pair is
// mergeable, that the drift is recorded on both sides, and that a fact written
// against the duplicate becomes visible through the canonical subject.
func TestCLIEntityMergeReconcilesDrift(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-drift.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-drift.json")

	payload := `{
  "episodes": [
    {"id":"ep-drift","kind":"note","content":"drift merge","source":{"kind":"note","external_ref":"thread-drift"}}
  ],
  "entities": [
    {"id":"project:yeoul","namespace":"","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:yeoul-dup","namespace":"repo","type":"Project","canonical_name":"Yeoul"}
  ],
  "facts": [
    {"id":"fact-drift","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul-dup","value_text":"written against the duplicate","supporting_episode_ids":["ep-drift"]}
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
		t.Fatalf("ingest json: %v", err)
	}

	preview, err := runCLI("entity", "merge-preview", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("merge-preview: %v", err)
	}
	if !strings.Contains(preview, `"drift_namespace": true`) {
		t.Fatalf("expected the preview to report namespace drift, got %q", preview)
	}

	if _, err := runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul", "--source", "project:yeoul-dup", "--reason", "recorded namespace drift"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	source, err := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-dup")
	if err != nil {
		t.Fatalf("entity get: %v", err)
	}
	if !strings.Contains(source, `"merge_drift_namespace"`) {
		t.Fatalf("expected the source to record namespace drift, got %q", source)
	}
	target, err := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul")
	if err != nil {
		t.Fatalf("entity get: %v", err)
	}
	if !strings.Contains(target, `"merge_drift_namespace"`) {
		t.Fatalf("expected the target to record namespace drift, got %q", target)
	}

	lookup, err := runCLI("fact", "lookup", "--db", dbPath, "--subject-id", "project:yeoul", "--json")
	if err != nil {
		t.Fatalf("fact lookup: %v", err)
	}
	if !strings.Contains(lookup, "fact-drift") {
		t.Fatalf("expected the duplicate's fact to be visible from the canonical subject, got %q", lookup)
	}
}

// TestCLIEntityMergePreviewReportsTypeDrift pins that a case-only type drift is
// grouped and reported without namespace being part of the group key.
func TestCLIEntityMergePreviewReportsTypeDrift(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "preview-type-drift.ltdb")
	ingestPath := filepath.Join(tmpDir, "preview-type-drift.json")

	payload := `{
  "entities": [
    {"id":"project:a","namespace":"repo","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:b","namespace":"repo","type":"project","canonical_name":"Yeoul"}
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
		t.Fatalf("ingest json: %v", err)
	}

	preview, err := runCLI("entity", "merge-preview", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("merge-preview: %v", err)
	}
	if !strings.Contains(preview, `"drift_type": true`) {
		t.Fatalf("expected the preview to report type drift, got %q", preview)
	}
	if strings.Contains(preview, `"drift_namespace": true`) {
		t.Fatalf("expected no namespace drift when both namespaces agree, got %q", preview)
	}
}

// TestCLIEntityMergeKeepsPopulatedNamespaceDriftApart pins that two populated
// namespaces that differ after folding stay separate candidates.
func TestCLIEntityMergeKeepsPopulatedNamespaceDriftApart(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "preview-namespace-split.ltdb")
	ingestPath := filepath.Join(tmpDir, "preview-namespace-split.json")

	payload := `{
  "entities": [
    {"id":"project:a","namespace":"repo-a","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:b","namespace":"repo-b","type":"Project","canonical_name":"Yeoul"}
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
		t.Fatalf("ingest json: %v", err)
	}

	preview, err := runCLI("entity", "merge-preview", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("merge-preview: %v", err)
	}
	if strings.Contains(preview, `"project:a"`) || strings.Contains(preview, `"project:b"`) {
		t.Fatalf("expected populated-but-different namespaces to stay separate, got %q", preview)
	}
}

// TestCLIEntityMergeKeepsNameDriftApart pins that a merge whose names do not
// overlap is still refused even when the namespace differs.
func TestCLIEntityMergeKeepsNameDriftApart(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "merge-name-mismatch.ltdb")
	ingestPath := filepath.Join(tmpDir, "merge-name-mismatch.json")

	payload := `{
  "entities": [
    {"id":"project:yeoul","namespace":"","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:other","namespace":"repo","type":"Project","canonical_name":"Something Else"}
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
		t.Fatalf("ingest json: %v", err)
	}

	_, err := runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul", "--source", "project:other", "--reason", "unrelated names")
	if err == nil {
		t.Fatal("expected a namespace-drift merge with unrelated names to be refused")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("expected the namespace rejection message, got %v", err)
	}
}
