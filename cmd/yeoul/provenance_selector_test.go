package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Conflicting provenance selectors must fail before any query instead of
// silently querying whichever selector was applied last.
func TestCLIProvenanceRejectsConflictingSelectors(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "provenance-selectors.ltdb")

	runCLI := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}
	if err := runCLI("init", "--db", dbPath); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCLI("ingest", "episode", "--db", dbPath, "--id", "ep-prov", "--kind", "note", "--content", "provenance selector anchor"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	conflicts := [][]string{
		{"--entity", "entity:a", "--fact", "fact:b"},
		{"--entity", "entity:a", "--episode", "ep-prov"},
		{"--fact", "fact:b", "--episode", "ep-prov"},
		{"--kind", "entity", "--id", "entity:a", "--entity", "entity:a"},
		{"--kind", "entity", "--id", "entity:a", "--fact", "fact:b"},
	}
	for _, selectors := range conflicts {
		t.Run(strings.Join(selectors, "_"), func(t *testing.T) {
			args := append([]string{"provenance", "--db", dbPath}, selectors...)
			if err := runCLI(args...); err == nil {
				t.Fatalf("expected conflicting selectors %v to fail", selectors)
			}
		})
	}

	// A single selector form still works.
	if err := runCLI("provenance", "--db", dbPath, "--episode", "ep-prov"); err != nil {
		t.Fatalf("expected a single selector to succeed: %v", err)
	}
	if err := runCLI("provenance", "--db", dbPath, "--kind", "episode", "--id", "ep-prov"); err != nil {
		t.Fatalf("expected --kind/--id to succeed: %v", err)
	}
	// Half a --kind/--id pair is still rejected.
	if err := runCLI("provenance", "--db", dbPath, "--kind", "episode"); err == nil {
		t.Fatal("expected --kind without --id to fail")
	}
}
