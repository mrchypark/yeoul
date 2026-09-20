package main

import (
	"context"
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
