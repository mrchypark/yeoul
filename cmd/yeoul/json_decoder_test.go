package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
)

// Every JSON entry point must reject trailing content and unknown fields
// without mutating the database.
func TestCLIJSONEntryPointsRejectMalformedDocuments(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "strict.ltdb")

	runCLI := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}
	mustRun := func(args ...string) {
		t.Helper()
		if err := runCLI(args...); err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	write := func(name, payload string) string {
		t.Helper()
		path := filepath.Join(tmpDir, name+".json")
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}

	mustRun("init", "--db", dbPath)
	seed := write("seed", `{"episodes":[{"id":"ep-seed","kind":"note","content":"seed","source":{"kind":"note","external_ref":"seed"}}]}`)
	mustRun("ingest", "json", "--db", dbPath, "--file", seed)

	validEpisode := `{"id":"ep-strict","kind":"note","content":"strict","source":{"kind":"note","external_ref":"strict"}}`
	bad := []struct {
		name    string
		payload string
	}{
		{"trailing value", `{"episodes":[` + validEpisode + `]} {"episodes":[]}`},
		{"trailing garbage", `{"episodes":[` + validEpisode + `]} not-json`},
		{"unknown top-level field", `{"episode":[` + validEpisode + `]}`},
		{"unknown record field", `{"episodes":[{"id":"ep-strict","kind":"note","content":"strict","source":{"kind":"note","external_ref":"strict"},"contnet":"typo"}]}`},
	}
	for _, testCase := range bad {
		for _, subcommand := range []string{"json", "batch"} {
			t.Run(testCase.name+"/"+subcommand, func(t *testing.T) {
				path := write(strings.ReplaceAll(testCase.name, " ", "-")+"-"+subcommand, testCase.payload)
				if err := runCLI("ingest", subcommand, "--db", dbPath, "--file", path); err == nil {
					t.Fatalf("expected ingest %s to reject %s", subcommand, testCase.payload)
				}
			})
		}
		t.Run(testCase.name+"/import", func(t *testing.T) {
			path := write(strings.ReplaceAll(testCase.name, " ", "-")+"-import", testCase.payload)
			if err := runCLI("admin", "import", "--confirm", "--db", dbPath, "--in", path); err == nil {
				t.Fatalf("expected admin import to reject %s", testCase.payload)
			}
		})
	}

	countsOut := &strings.Builder{}
	if err := run(ctx, []string{"inspect", "counts", "--db", dbPath, "--json"}, countsOut, countsOut); err != nil {
		t.Fatalf("inspect counts: %v", err)
	}
	var counts inspectCountsResult
	if err := json.Unmarshal([]byte(countsOut.String()), &counts); err != nil {
		t.Fatalf("unmarshal counts: %v\noutput=%s", err, countsOut.String())
	}
	if counts.Counts["episodes"] != 1 {
		t.Fatalf("expected malformed payloads to leave one seeded episode, got %d", counts.Counts["episodes"])
	}
	if err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-strict"); err == nil {
		t.Fatal("expected the rejected episode to be absent")
	}
}

func TestDecodeSingleJSONAcceptsOneDocument(t *testing.T) {
	var payload ingestJSONFile
	if err := decodeSingleJSON([]byte(`{"episodes":[{"id":"ep-1","kind":"note","content":"ok"}]}`), &payload); err != nil {
		t.Fatalf("decode single document: %v", err)
	}
	if len(payload.Episodes) != 1 || payload.Episodes[0].ID != "ep-1" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if err := decodeSingleJSON([]byte(`{"episodes":[]} `), &payload); err != nil {
		t.Fatalf("expected trailing whitespace to be accepted: %v", err)
	}
}
