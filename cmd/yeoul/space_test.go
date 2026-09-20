package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
)

// Records ingested into a non-default space must be reachable through the
// ordinary get/search/lookup reads once --space is supplied, and reads must not
// mix records across spaces.
func TestCLISpaceSelectsRecordsAcrossReads(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "spaces.ltdb")

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
	write := func(name, payload string) string {
		t.Helper()
		path := filepath.Join(tmpDir, name+".json")
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}

	mustRun("init", "--db", dbPath)

	// alpha records name their own space explicitly in the JSON payload.
	alpha := write("alpha", `{
  "episodes":[{"id":"ep-alpha","space_id":"alpha","kind":"note","content":"alpha unique token zzzalpha","source":{"kind":"note","external_ref":"alpha"}}],
  "entities":[{"id":"entity:alpha","space_id":"alpha","type":"Project","canonical_name":"Alpha"}],
  "facts":[{"id":"fact-alpha","space_id":"alpha","predicate":"HAS_STATUS","subject_id":"entity:alpha","value_text":"alpha status","supporting_episode_ids":["ep-alpha"]}]
}`)
	mustRun("ingest", "json", "--db", dbPath, "--file", alpha)

	// beta records omit the space and take the --space default.
	beta := write("beta", `{
  "episodes":[{"id":"ep-beta","kind":"note","content":"beta unique token zzzbeta","source":{"kind":"note","external_ref":"beta"}}],
  "entities":[{"id":"entity:beta","type":"Project","canonical_name":"Beta"}],
  "facts":[{"id":"fact-beta","predicate":"HAS_STATUS","subject_id":"entity:beta","value_text":"beta status","supporting_episode_ids":["ep-beta"]}]
}`)
	mustRun("ingest", "json", "--db", dbPath, "--file", beta, "--space", "beta")

	// get: each space resolves only its own record.
	if _, err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-alpha", "--space", "alpha"); err != nil {
		t.Fatalf("get alpha episode: %v", err)
	}
	if _, err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-beta", "--space", "beta"); err != nil {
		t.Fatalf("get beta episode: %v", err)
	}
	if _, err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-alpha"); err == nil {
		t.Fatal("expected alpha record to be unreachable in the default space")
	}
	if _, err := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-alpha", "--space", "beta"); err == nil {
		t.Fatal("expected alpha record to be unreachable in the beta space")
	}

	// search: hits stay within the selected space.
	assertSearch := func(query, space, wantID, avoidID string) {
		t.Helper()
		out := mustRun("search", "--db", dbPath, "--query", query, "--space", space, "--json")
		var resp yeoulSearchResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("unmarshal search: %v\noutput=%s", err, out)
		}
		found, mixed := false, false
		for _, hit := range resp.Hits {
			if hit.RecordID == wantID {
				found = true
			}
			if hit.RecordID == avoidID {
				mixed = true
			}
		}
		if !found {
			t.Fatalf("search %q in space %s did not return %s: %s", query, space, wantID, out)
		}
		if mixed {
			t.Fatalf("search %q in space %s mixed in %s: %s", query, space, avoidID, out)
		}
	}
	assertSearch("zzzalpha", "alpha", "ep-alpha", "ep-beta")
	assertSearch("zzzbeta", "beta", "ep-beta", "ep-alpha")

	// fact lookup: facts stay within the selected space.
	assertLookup := func(space, wantID, avoidID string) {
		t.Helper()
		out := mustRun("fact", "lookup", "--db", dbPath, "--space", space, "--json")
		var resp yeoulFactLookupResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("unmarshal lookup: %v\noutput=%s", err, out)
		}
		found, mixed := false, false
		for _, fact := range resp.Facts {
			if fact.ID == wantID {
				found = true
			}
			if fact.ID == avoidID {
				mixed = true
			}
		}
		if !found {
			t.Fatalf("lookup in space %s did not return %s: %s", space, wantID, out)
		}
		if mixed {
			t.Fatalf("lookup in space %s mixed in %s: %s", space, avoidID, out)
		}
	}
	assertLookup("alpha", "fact-alpha", "fact-beta")
	assertLookup("beta", "fact-beta", "fact-alpha")
}

type yeoulSearchResponse struct {
	Hits []struct {
		RecordID string `json:"record_id"`
	} `json:"hits"`
}

type yeoulFactLookupResponse struct {
	Facts []struct {
		ID string `json:"id"`
	} `json:"facts"`
}
