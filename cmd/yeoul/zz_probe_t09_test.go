package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeT09StaleSequence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "probe.ltdb")
	ingestPath := filepath.Join(tmpDir, "ingest.json")
	payload := `{
  "episodes": [{"id":"ep-1","kind":"note","content":"first note","source":{"kind":"note"}}],
  "entities": [{"id":"project:yeoul","type":"Project","canonical_name":"yeoul"}],
  "facts": [{"id":"fact-1","predicate":"DESCRIBES","subject_id":"project:yeoul","value_text":"first fact","supporting_episode_ids":["ep-1"]}]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI := func(args ...string) string {
		t.Helper()
		var stdout, stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
		return stdout.String()
	}
	runCLI("init", "--db", dbPath)
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)

	// Take a manifest snapshot by building the managed store once.
	root, _, err := managedRaxIndexPaths(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("managed root: %s", root)

	info := func(tag string) {
		st, err := os.Stat(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: isDir=%v size=%d mtime=%s", tag, st.IsDir(), st.Size(), st.ModTime().Format(time.RFC3339Nano))
	}
	info("before")

	// Ordinary CLI write: ingest a second episode + fact into the same DB.
	payload2 := `{
  "episodes": [{"id":"ep-2","kind":"note","content":"second note","source":{"kind":"note"}}],
  "facts": [{"id":"fact-2","predicate":"DESCRIBES","subject_id":"project:yeoul","value_text":"second fact","supporting_episode_ids":["ep-2"]}]
}`
	if err := os.WriteFile(ingestPath, []byte(payload2), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	info("after-write")

	listEntries := func(tag string) map[string]string {
		out := map[string]string{}
		entries, err := os.ReadDir(dbPath)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		for _, e := range entries {
			i, _ := e.Info()
			out[e.Name()] = i.ModTime().Format(time.RFC3339Nano)
		}
		t.Logf("%s entries: %v", tag, out)
		return out
	}
	_ = listEntries("after-write")

	// Now try a write that may only modify existing files in place.
	stBefore, _ := os.Stat(dbPath)
	payload3 := `{"episodes":[{"id":"ep-3","kind":"note","content":"third note","source":{"kind":"note"}}]}`
	if err := os.WriteFile(ingestPath, []byte(payload3), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	stAfter, _ := os.Stat(dbPath)
	t.Logf("in-place write: size %d->%d mtime %s->%s unchanged=%v", stBefore.Size(), stAfter.Size(), stBefore.ModTime().Format(time.RFC3339Nano), stAfter.ModTime().Format(time.RFC3339Nano), stBefore.ModTime().Equal(stAfter.ModTime()) && stBefore.Size() == stAfter.Size())
}
