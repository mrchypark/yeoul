package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveT09RaxRuntime finds a rax runtime this test can actually drive.
// lookupRaxRuntime resolves a library bundled beside the running executable,
// so under `go test` it only succeeds when YEOUL_RAX_LIB or YEOUL_RAX_BIN is
// set. Otherwise fall back to the newest runtime of the installed CLI, and
// report when none exists so the caller can skip instead of passing vacuously.
func resolveT09RaxRuntime(t *testing.T, home string) (raxRuntime, bool) {
	t.Helper()
	if rt, ok := lookupRaxRuntime("", ""); ok {
		return rt, true
	}
	if paths := installedRaxLibraryPaths(home); len(paths) > 0 {
		return raxRuntime{Kind: "ffi", Path: paths[0]}, true
	}
	return raxRuntime{}, false
}

// TestProbeT09ManagedStoreStaleAfterOrdinaryCLIWrite closes review issue T-09:
// a managed rax index must not stay silently stale after an ordinary CLI write.
//
// Observed behavior that this pins: managedRaxStoreFresh is a database stat
// check (manifest.DatabaseSize/DatabaseModTime against the live path), and an
// ordinary `yeoul ingest` moves that stat, so the managed store is fresh right
// after ensureManagedRaxStore builds it and stale after the next ordinary
// write. The managed store therefore tracks writes through the stat check
// rather than through an explicit revision: there is no silent stale window,
// but there is also no incremental refresh, so the next search rebuilds it.
func TestProbeT09ManagedStoreStaleAfterOrdinaryCLIWrite(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	// managedRaxIndexPaths roots the managed store under os.UserCacheDir, so
	// redirect the cache into the temp dir and never touch the real one.
	realHome, _ := os.UserHomeDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

	runtime, ok := resolveT09RaxRuntime(t, realHome)
	if !ok {
		t.Skipf("no rax runtime available: set YEOUL_RAX_LIB or install Yeoul so %s exists", raxLibraryName())
	}

	dbPath := filepath.Join(tmpDir, "probe.ltdb")
	ingestPath := filepath.Join(tmpDir, "ingest.json")
	runCLI := func(args ...string) {
		t.Helper()
		var stdout, stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
	}
	ingest := func(payload string) {
		t.Helper()
		if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
			t.Fatalf("write ingest payload: %v", err)
		}
		runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	}

	runCLI("init", "--db", dbPath)
	ingest(`{
  "episodes": [{"id":"ep-1","kind":"note","content":"first note","source":{"kind":"note"}}],
  "entities": [{"id":"project:yeoul","type":"Project","canonical_name":"yeoul"}],
  "facts": [{"id":"fact-1","predicate":"DESCRIBES","subject_id":"project:yeoul","value_text":"first fact","supporting_episode_ids":["ep-1"]}]
}`)

	root, storePath, err := managedRaxIndexPaths(dbPath)
	if err != nil {
		t.Fatalf("managedRaxIndexPaths: %v", err)
	}
	_, empty, err := ensureManagedRaxStore(ctx, nil, dbPath, runtime)
	if err != nil {
		t.Fatalf("ensureManagedRaxStore: %v", err)
	}
	if empty {
		t.Fatalf("expected a non-empty managed rax store for a database with records")
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("managed rax store missing after build: %v", err)
	}
	fresh, err := managedRaxStoreFresh(root, storePath, dbPath, runtime)
	if err != nil {
		t.Fatalf("managedRaxStoreFresh after build: %v", err)
	}
	if !fresh {
		t.Fatalf("expected the freshly built managed rax store to be fresh")
	}
	built, err := readProjectionManifest(root)
	if err != nil {
		t.Fatalf("read manifest after build: %v", err)
	}
	if built.ProjectionCount != 3 {
		t.Fatalf("expected 3 projections for 1 episode + 1 entity + 1 fact, got %d", built.ProjectionCount)
	}

	// Ordinary CLI write: a second episode and fact, with no index or search
	// command in between, which is exactly the T-09 scenario.
	ingest(`{
	  "episodes": [{"id":"ep-2","kind":"note","content":"second note","source":{"kind":"note"}}],
	  "facts": [{"id":"fact-2","predicate":"DESCRIBES","subject_id":"project:yeoul","value_text":"second fact","supporting_episode_ids":["ep-2"]}]
	}`)

	// Staleness is detected by comparing the manifest's recorded stat with the
	// live path, so the write has to move that stat for the check to see it.
	stat, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db after write: %v", err)
	}
	if stat.Size() == built.DatabaseSize && stat.ModTime().Equal(built.DatabaseModTime) {
		t.Fatalf("ordinary CLI write left the database stat unchanged (size=%d mtime=%s); the stat-based freshness check cannot detect it", stat.Size(), stat.ModTime())
	}
	fresh, err = managedRaxStoreFresh(root, storePath, dbPath, runtime)
	if err != nil {
		t.Fatalf("managedRaxStoreFresh after write: %v", err)
	}
	if fresh {
		t.Fatalf("expected the ordinary CLI write to mark the managed rax store stale")
	}
}
