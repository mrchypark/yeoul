package main

import (
	"context"
	"os"
	"path/filepath"
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
			name: "case-different names stay distinct",
			entities: []yeoul.EntityInput{
				entity("person:a", "Alex", nil),
				entity("person:b", "alex", nil),
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
    {"id":"person:delta","type":"Person","canonical_name":"Alex","stable_key":"gamma-1"}
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
}
