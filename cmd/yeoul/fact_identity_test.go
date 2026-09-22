package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func TestCLIFactAssertRejectsConflictingDerivedIdentity(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "identity-conflict.ltdb")

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
	if _, err := runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-identity",
		"--kind", "note",
		"--content", "identity",
		"--source-kind", "note",
		"--source-external-ref", "identity",
	); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if _, err := runCLI(
		"fact", "assert",
		"--db", dbPath,
		"--predicate", "HAS_KEY",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-name", "Display A",
		"--subject-stable-key", "key",
		"--value-text", "first",
		"--supporting-episodes", "ep-identity",
		"--json",
	); err != nil {
		t.Fatalf("first assert: %v", err)
	}

	_, err := runCLI(
		"fact", "assert",
		"--db", dbPath,
		"--predicate", "HAS_KEY",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-name", "key",
		"--value-text", "second",
		"--supporting-episodes", "ep-identity",
		"--json",
	)
	if err == nil {
		t.Fatal("expected the conflicting automatic upsert to be rejected")
	}

	entityID := yeoul.EntityID("", "Feature", "key")
	entity, err := runCLI("entity", "get", "--db", dbPath, "--id", entityID, "--json")
	if err != nil {
		t.Fatalf("entity get: %v", err)
	}
	if !strings.Contains(entity, "Display A") || !strings.Contains(entity, `"stable_key": "key"`) {
		t.Fatalf("expected the stored identity to stay unchanged, got %q", entity)
	}
}

func TestCLIFactAssertReusesSamePendingAutomaticEntity(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "identity-same-batch.ltdb")
	var stdout, stderr strings.Builder
	runCLI := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return run(ctx, args, &stdout, &stderr)
	}
	if err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCLI("ingest", "episode", "--db", dbPath, "--id", "ep-same-batch", "--kind", "note", "--content", "same batch", "--source-kind", "note", "--source-external-ref", "same-batch"); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if err := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "SAME_ENTITY", "--upsert-subject", "--subject-type", "Project", "--subject-name", "Yeoul",
		"--upsert-object", "--object-type", "Project", "--object-name", "Yeoul",
		"--supporting-episodes", "ep-same-batch", "--json"); err != nil {
		t.Fatalf("assert same pending entity: %v\nstderr=%s", err, stderr.String())
	}
	assertedOut := stdout.String()
	if err := runCLI("inspect", "counts", "--db", dbPath, "--json"); err != nil {
		t.Fatalf("counts: %v", err)
	}
	if !strings.Contains(stdout.String(), `"entities": 1`) || !strings.Contains(stdout.String(), `"facts": 1`) {
		t.Fatalf("expected one reused entity and one fact, got %q", stdout.String())
	}
	if !strings.Contains(assertedOut, `"subject_id": "`+yeoul.EntityID("", "Project", "Yeoul")+`"`) || !strings.Contains(assertedOut, `"object_id": "`+yeoul.EntityID("", "Project", "Yeoul")+`"`) {
		t.Fatalf("expected both endpoints to use the same entity, got %q", assertedOut)
	}
}

func TestCLIFactAssertRejectsPendingNamespaceDriftAtomically(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "identity-pending-namespace-drift.ltdb")
	var stdout, stderr strings.Builder
	runCLI := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return run(ctx, args, &stdout, &stderr)
	}
	if err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCLI("ingest", "episode", "--db", dbPath, "--id", "ep-pending-drift", "--kind", "note", "--content", "pending drift", "--source-kind", "note", "--source-external-ref", "pending-drift"); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	err := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "SAME_PROJECT", "--upsert-subject", "--subject-type", "Project", "--subject-name", "Yeoul",
		"--upsert-object", "--object-namespace", "repo", "--object-type", "Project", "--object-name", "Yeoul",
		"--supporting-episodes", "ep-pending-drift", "--json")
	var apiErr *yeoul.Error
	if !errors.As(err, &apiErr) || apiErr.Code != yeoul.ErrEntityNearDuplicate {
		t.Fatalf("expected %s, got %v", yeoul.ErrEntityNearDuplicate, err)
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if err := runCLI("inspect", "counts", "--db", dbPath, "--json"); err != nil {
		t.Fatalf("counts: %v", err)
	}
	if !strings.Contains(stdout.String(), `"entities": 0`) || !strings.Contains(stdout.String(), `"facts": 0`) {
		t.Fatalf("pending near-duplicate must leave no entity or fact writes, got %q", stdout.String())
	}
}
