package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
)

// Supplied revision history cannot be restored through CLI ingest, so a
// payload that carries it must fail loudly instead of silently dropping the
// history and reporting success.
func TestCLIIngestJSONRejectsSuppliedRevisionHistory(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "revisions.ltdb")

	runCLI := func(args ...string) (string, error) {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		err := run(ctx, args, &stdout, &stderr)
		return stdout.String(), err
	}
	mustRun := func(args ...string) string {
		t.Helper()
		out, err := runCLI(args...)
		if err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
		return out
	}
	writePayload := func(name, payload string) string {
		t.Helper()
		path := filepath.Join(tmpDir, name+".json")
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("write payload %s: %v", name, err)
		}
		return path
	}

	mustRun("init", "--db", dbPath)
	seedPath := writePayload("seed", `{"episodes":[{"id":"ep-seed","kind":"note","content":"seeded record","source":{"kind":"note","external_ref":"seed"}}]}`)
	mustRun("ingest", "json", "--db", dbPath, "--file", seedPath)

	episode := `{"id":"ep-revisions","kind":"note","content":"record with history","source":{"kind":"note","external_ref":"revisions"}}`
	entityRevision := `{"id":"entityrev:1","entity_id":"entity:restore","type":"Project","canonical_name":"Restore"}`
	factRevision := `{"id":"factrev:1","fact_id":"fact:restore","predicate":"HAS_STATUS","subject_id":"entity:restore","status":"active"}`
	cases := []struct {
		name    string
		payload string
	}{
		{"records with entity revisions", `{"episodes":[` + episode + `],"entity_revisions":[` + entityRevision + `]}`},
		{"records with fact revisions", `{"episodes":[` + episode + `],"fact_revisions":[` + factRevision + `]}`},
		{"entity revisions only", `{"entity_revisions":[` + entityRevision + `]}`},
		{"fact revisions only", `{"fact_revisions":[` + factRevision + `]}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writePayload(strings.ReplaceAll(testCase.name, " ", "-"), testCase.payload)
			for _, subcommand := range []string{"json", "batch"} {
				if _, err := runCLI("ingest", subcommand, "--db", dbPath, "--file", path); err == nil {
					t.Fatalf("ingest %s accepted supplied revision history payload %s", subcommand, testCase.payload)
				}
			}
		})
	}

	countsJSON := mustRun("inspect", "counts", "--db", dbPath, "--json")
	var counts inspectCountsResult
	if err := json.Unmarshal([]byte(countsJSON), &counts); err != nil {
		t.Fatalf("unmarshal counts: %v\noutput=%s", err, countsJSON)
	}
	if counts.Counts["episodes"] != 1 {
		t.Fatalf("expected the rejected payloads to leave one seeded episode, got %d", counts.Counts["episodes"])
	}
	if _, err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-revisions"); err == nil {
		t.Fatal("expected the episode from a rejected payload to be absent")
	}
}
