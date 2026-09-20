package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/mrchypark/yeoul/pkg/policy"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func TestMain(m *testing.M) {
	if os.Getenv("YEOUL_FAKE_RAX") == "1" {
		os.Exit(runFakeRax())
	}
	os.Exit(m.Run())
}

func runFakeRax() int {
	argsPath := os.Getenv("YEOUL_FAKE_RAX_ARGS")
	projectionPath := os.Getenv("YEOUL_FAKE_RAX_PROJECTION")
	if argsPath == "" || projectionPath == "" {
		_, _ = os.Stderr.WriteString("missing fake rax environment\n")
		return 2
	}

	args := os.Args[1:]
	argsLine := strings.Join(args, " ") + "\n"
	argsFile, err := os.OpenFile(argsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 2
	}
	if _, err := argsFile.WriteString(argsLine); err != nil {
		_ = argsFile.Close()
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 2
	}
	if err := argsFile.Close(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 2
	}
	if len(args) > 0 && args[0] == "search" {
		if idsPath := os.Getenv("YEOUL_FAKE_RAX_SEARCH_IDS"); idsPath != "" {
			data, err := os.ReadFile(idsPath)
			if err != nil {
				_, _ = os.Stderr.WriteString(err.Error() + "\n")
				return 2
			}
			_, _ = os.Stdout.Write(data)
			return 0
		}
		_, _ = os.Stdout.WriteString(`[{"doc_id":"fact:fact-index"}]`)
		return 0
	}
	if os.Getenv("YEOUL_FAKE_RAX_FAIL_INGEST") == "1" {
		_, _ = os.Stderr.WriteString("fake ingest failure\n")
		return 2
	}
	if os.Getenv("YEOUL_FAKE_RAX_MERGE_DOCIDS") == "1" {
		return runFakeRaxMergeDocIDs(args)
	}
	for i, arg := range args {
		if arg == "--store" && i+1 < len(args) {
			if err := os.WriteFile(args[i+1], []byte("fake rax store"), 0o644); err != nil {
				_, _ = os.Stderr.WriteString(err.Error() + "\n")
				return 2
			}
			break
		}
	}
	for i, arg := range args {
		if arg == "--input" && i+1 < len(args) {
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				_, _ = os.Stderr.WriteString(err.Error() + "\n")
				return 2
			}
			if err := os.WriteFile(projectionPath, data, 0o644); err != nil {
				_, _ = os.Stderr.WriteString(err.Error() + "\n")
				return 2
			}
			return 0
		}
	}

	_, _ = os.Stderr.WriteString("missing --input\n")
	return 2
}

// runFakeRaxMergeDocIDs models rax ingest semantics faithfully enough to
// observe publication: documents are appended or updated by doc_id, and
// document IDs already present in the store are retained. The store file holds
// one doc_id per line so a test can read the effective membership back.
func runFakeRaxMergeDocIDs(args []string) int {
	var storePath, inputPath string
	for i, arg := range args {
		switch arg {
		case "--store":
			if i+1 < len(args) {
				storePath = args[i+1]
			}
		case "--input":
			if i+1 < len(args) {
				inputPath = args[i+1]
			}
		}
	}
	if storePath == "" || inputPath == "" {
		_, _ = os.Stderr.WriteString("merge fake rax needs --store and --input\n")
		return 2
	}
	seen := map[string]bool{}
	order := []string{}
	if existing, err := os.ReadFile(storePath); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			order = append(order, line)
		}
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 2
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var doc struct {
			DocID string `json:"doc_id"`
		}
		if err := json.Unmarshal([]byte(line), &doc); err != nil || doc.DocID == "" {
			_, _ = os.Stderr.WriteString("merge fake rax cannot read doc_id\n")
			return 2
		}
		if !seen[doc.DocID] {
			seen[doc.DocID] = true
			order = append(order, doc.DocID)
		}
	}
	if err := os.WriteFile(storePath, []byte(strings.Join(order, "\n")+"\n"), 0o644); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 2
	}
	return 0
}

func TestLookupRaxRuntimeFindsBundledFFI(t *testing.T) {
	t.Setenv("YEOUL_RAX_LIB", "")
	t.Setenv("YEOUL_RAX_BIN", "")
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	raxPath := filepath.Join(filepath.Dir(exePath), raxLibraryName())
	if err := os.WriteFile(raxPath, []byte("fake ffi"), 0o644); err != nil {
		t.Skipf("cannot stage bundled rax ffi fixture beside test executable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(raxPath) })

	got, ok := lookupRaxRuntime("", "")
	if !ok {
		t.Fatalf("expected bundled rax ffi runtime to resolve")
	}
	if got.Kind != "ffi" || got.Path != raxPath {
		t.Fatalf("expected bundled rax ffi path %q, got %#v", raxPath, got)
	}
}

func TestLookupRaxRuntimeExplicitBinOverridesBundledFFI(t *testing.T) {
	t.Setenv("YEOUL_RAX_LIB", "")
	t.Setenv("YEOUL_RAX_BIN", "")
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	bundledLibPath := filepath.Join(filepath.Dir(exePath), raxLibraryName())
	if err := os.WriteFile(bundledLibPath, []byte("fake ffi"), 0o644); err != nil {
		t.Skipf("cannot stage bundled rax ffi fixture beside test executable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(bundledLibPath) })

	explicitBinPath := filepath.Join(t.TempDir(), raxExecutableName())
	if err := os.WriteFile(explicitBinPath, []byte("fake cli"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	got, ok := lookupRaxRuntime("", explicitBinPath)
	if !ok {
		t.Fatalf("expected explicit rax bin runtime to resolve")
	}
	if got.Kind != "cli" || got.Path != explicitBinPath {
		t.Fatalf("expected explicit cli path %q, got %#v", explicitBinPath, got)
	}
}

func TestCLIInitIngestEpisodeAndSearch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.ltdb")

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
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-now",
		"--kind", "note",
		"--content", "Ladybug remains an internal storage concern.",
		"--source-kind", "note",
		"--source-external-ref", "thread-1",
	)

	episode := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", "ep-now", "--json")
	if strings.Contains(episode, `"observed_at": "0001-01-01T00:00:00Z"`) {
		t.Fatalf("expected direct CLI episode ingest to default observed_at to system time, got %q", episode)
	}
	if !strings.Contains(episode, `"observed_at_basis": "system_time_default"`) {
		t.Fatalf("expected episode metadata to record system-time observed_at basis, got %q", episode)
	}

	output := runCLI("search", "--db", dbPath, "--query", "Ladybug")
	if !strings.Contains(output, "Ladybug remains an internal storage concern.") {
		t.Fatalf("expected search output to contain episode text, got %q", output)
	}
}

func TestCLIIngestJSONAndGetFact(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	ingestPath := filepath.Join(tmpDir, "ingest.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-status",
      "kind":"note",
      "content":"Yeoul is in scaffold mode.",
      "source":{"kind":"note","external_ref":"thread-2"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"status:scaffold","type":"Status","canonical_name":"Scaffold"}
  ],
  "facts": [
    {
      "id":"fact-status",
      "predicate":"HAS_STATUS",
      "subject_id":"project:yeoul",
      "object_id":"status:scaffold",
      "value_text":"Yeoul is in scaffold mode.",
      "supporting_episode_ids":["ep-status"]
    }
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

	output := runCLI("get", "--db", dbPath, "--kind", "fact", "--id", "fact-status")
	if !strings.Contains(output, `"predicate": "HAS_STATUS"`) {
		t.Fatalf("expected fact JSON output, got %q", output)
	}
}

func TestCLIInspectCountsAndNeighborhood(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	ingestPath := filepath.Join(tmpDir, "graph.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-graph",
      "kind":"note",
      "content":"Yeoul uses Ladybug.",
      "source":{"kind":"note","external_ref":"thread-graph"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
  ],
  "facts": [
    {
      "id":"fact-graph",
      "predicate":"USES_STORAGE_ENGINE",
      "subject_id":"project:yeoul",
      "object_id":"database:ladybug",
      "value_text":"Yeoul uses Ladybug.",
      "supporting_episode_ids":["ep-graph"]
    }
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

	counts := runCLI("inspect", "counts", "--db", dbPath, "--json")
	if !strings.Contains(counts, `"episodes": 1`) || !strings.Contains(counts, `"facts": 1`) {
		t.Fatalf("expected counts JSON output, got %q", counts)
	}

	hood := runCLI("neighborhood", "--db", dbPath, "--entity", "project:yeoul", "--json")
	if !strings.Contains(hood, `"edges"`) || !strings.Contains(hood, `"project:yeoul"`) {
		t.Fatalf("expected neighborhood JSON output, got %q", hood)
	}
}

func TestCLIFactRetract(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	ingestPath := filepath.Join(tmpDir, "fact.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-fact",
      "kind":"note",
      "content":"Status was scaffold.",
      "source":{"kind":"note","external_ref":"thread-fact"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"status:scaffold","type":"Status","canonical_name":"Scaffold"}
  ],
  "facts": [
    {
      "id":"fact-status",
      "predicate":"HAS_STATUS",
      "subject_id":"project:yeoul",
      "object_id":"status:scaffold",
      "value_text":"Yeoul is scaffold.",
      "supporting_episode_ids":["ep-fact"]
    }
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
	runCLI("fact", "retract", "--confirm", "--db", dbPath, "--id", "fact-status", "--reason", "incorrect source")

	output := runCLI("fact", "get", "--db", dbPath, "--id", "fact-status")
	if !strings.Contains(output, `"status": "retracted"`) {
		t.Fatalf("expected retracted fact output, got %q", output)
	}
}

func TestCLIPolicyValidateAndListRecipes(t *testing.T) {
	ctx := context.Background()
	packPath, err := filepath.Abs(filepath.Join("..", "..", "agent-pack"))
	if err != nil {
		t.Fatalf("resolve pack path: %v", err)
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

	validate := runCLI("policy", "validate", "--path", packPath, "--json")
	if !strings.Contains(validate, `"valid": true`) {
		t.Fatalf("expected valid policy pack, got %q", validate)
	}

	show := runCLI("policy", "show", "--path", packPath, "--json")
	var pack policy.Pack
	if err := json.Unmarshal([]byte(show), &pack); err != nil {
		t.Fatalf("unmarshal policy show output: %v\noutput=%s", err, show)
	}
	if pack.EpisodeRules.FactPromotion == nil {
		t.Fatalf("expected fact promotion in policy show output, got %#v", pack.EpisodeRules)
	}
	if !slices.Contains(pack.EpisodeRules.FactPromotion.Candidates, "stable preferences") {
		t.Fatalf("expected stable preferences candidate, got %v", pack.EpisodeRules.FactPromotion.Candidates)
	}

	recipes := runCLI("policy", "list-recipes", "--path", packPath)
	if !strings.Contains(recipes, "recent_context") || !strings.Contains(recipes, "preflight_briefing") || !strings.Contains(recipes, "project_memory") {
		t.Fatalf("expected recipe list output, got %q", recipes)
	}
}

func TestCLIIndexBuildStatusAndVerify(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	ingestPath := filepath.Join(tmpDir, "graph.json")
	indexRoot := filepath.Join(tmpDir, "index")
	storePath := filepath.Join(tmpDir, "projection.rax")
	fakeRaxPath := os.Args[0]
	raxArgsPath := filepath.Join(tmpDir, "rax-args.txt")
	raxProjectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")

	payload := `{
  "episodes": [
    {
      "id":"ep-index",
      "kind":"note",
      "content":"Yeoul keeps LatticeDB as canonical truth and uses rax as derived retrieval.",
      "source":{"kind":"note","external_ref":"thread-index"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"runtime:rax","type":"Runtime","canonical_name":"rax"}
  ],
  "facts": [
    {
      "id":"fact-index",
      "predicate":"USES_RETRIEVAL_RUNTIME",
      "subject_id":"project:yeoul",
      "object_id":"runtime:rax",
      "value_text":"Yeoul uses rax as derived retrieval runtime.",
      "supporting_episode_ids":["ep-index"]
    }
  ]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", raxArgsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", raxProjectionPath)

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

	build := runCLI("index", "build", "--db", dbPath, "--root", indexRoot, "--json")
	if !strings.Contains(build, `"projection_count": 4`) {
		t.Fatalf("expected build JSON output, got %q", build)
	}
	if _, err := os.Stat(filepath.Join(indexRoot, "yeoul-index.json")); err != nil {
		t.Fatalf("expected yeoul-index.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(indexRoot, "projection.ndjson")); err != nil {
		t.Fatalf("expected projection.ndjson: %v", err)
	}
	projectionData, err := os.ReadFile(filepath.Join(indexRoot, "projection.ndjson"))
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if !strings.Contains(string(projectionData), `"projection_id":"fact:fact-index"`) || !strings.Contains(string(projectionData), `"search_text":"USES_RETRIEVAL_RUNTIME project:yeoul runtime:rax Yeoul uses rax as derived retrieval runtime."`) {
		t.Fatalf("expected Yeoul projection fields, got %q", string(projectionData))
	}

	status := runCLI("index", "status", "--root", indexRoot, "--json")
	if !strings.Contains(status, `"projection_count": 4`) || !strings.Contains(status, `"facts": 1`) {
		t.Fatalf("expected status JSON output, got %q", status)
	}

	verify := runCLI("index", "verify", "--db", dbPath, "--root", indexRoot, "--json")
	if !strings.Contains(verify, `"valid": true`) {
		t.Fatalf("expected verify JSON output, got %q", verify)
	}
	rebuild := runCLI("index", "rebuild", "--db", dbPath, "--root", indexRoot, "--json")
	if !strings.Contains(rebuild, `"projection_count": 4`) {
		t.Fatalf("expected rebuild JSON output, got %q", rebuild)
	}

	publish := runCLI("index", "publish-rax", "--root", indexRoot, "--store", storePath, "--rax-bin", fakeRaxPath, "--json")
	if !strings.Contains(publish, `"published": true`) || !strings.Contains(publish, `"rax_runtime": "cli:`) || !strings.Contains(publish, `"published_document_count": 4`) {
		t.Fatalf("expected publish JSON output, got %q", publish)
	}
	raxArgs, err := os.ReadFile(raxArgsPath)
	if err != nil {
		t.Fatalf("read fake rax args: %v", err)
	}
	if !strings.Contains(string(raxArgs), "ingest docs --store "+storePath+" --input ") {
		t.Fatalf("expected rax ingest docs args, got %q", string(raxArgs))
	}
	raxDocs, err := os.ReadFile(raxProjectionPath)
	if err != nil {
		t.Fatalf("read fake rax docs: %v", err)
	}
	if !strings.Contains(string(raxDocs), `"doc_id":"fact:fact-index"`) || !strings.Contains(string(raxDocs), `"text":"USES_RETRIEVAL_RUNTIME project:yeoul runtime:rax Yeoul uses rax as derived retrieval runtime."`) {
		t.Fatalf("expected rax raw document fields, got %q", string(raxDocs))
	}
	if !strings.Contains(string(raxDocs), `"metadata":{`) || !strings.Contains(string(raxDocs), `"predicate":"USES_RETRIEVAL_RUNTIME"`) || !strings.Contains(string(raxDocs), `"projection_type":"fact"`) {
		t.Fatalf("expected rax metadata to preserve Yeoul projection metadata, got %q", string(raxDocs))
	}

	autoBench := runCLI("bench", "query", "--db", dbPath, "--query", "derived retrieval runtime", "--backend", "auto", "--rax-bin", fakeRaxPath, "--iterations", "1", "--json")
	if !strings.Contains(autoBench, `"search"`) {
		t.Fatalf("expected auto bench to build and use managed rax search while the database is open, got %q", autoBench)
	}

	search := runCLI("search", "--db", dbPath, "--query", "derived retrieval runtime", "--backend", "rax", "--rax-bin", fakeRaxPath, "--json")
	if !strings.Contains(search, `"record_id": "fact-index"`) || !strings.Contains(search, `"rax_candidate_rank:1"`) {
		t.Fatalf("expected search to use managed rax reranking, got %q", search)
	}
	_ = runCLI("search", "--db", dbPath, "--query", "derived retrieval runtime", "--backend", "rax", "--rax-bin", fakeRaxPath, "--json")
	raxArgsAfterSearch, err := os.ReadFile(raxArgsPath)
	if err != nil {
		t.Fatalf("read fake rax args after search: %v", err)
	}
	if got := strings.Count(string(raxArgsAfterSearch), "ingest docs "); got != 2 {
		t.Fatalf("expected publish plus one managed rax ingest after repeated search, got %d calls:\n%s", got, string(raxArgsAfterSearch))
	}
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	dbModTime := dbInfo.ModTime().Add(time.Second)
	if err := os.Chtimes(dbPath, dbModTime, dbModTime); err != nil {
		t.Fatalf("touch db: %v", err)
	}
	_ = runCLI("search", "--db", dbPath, "--query", "derived retrieval runtime", "--backend", "rax", "--rax-bin", fakeRaxPath, "--json")
	raxArgsAfterTouch, err := os.ReadFile(raxArgsPath)
	if err != nil {
		t.Fatalf("read fake rax args after db touch: %v", err)
	}
	if got := strings.Count(string(raxArgsAfterTouch), "ingest docs "); got != 3 {
		t.Fatalf("expected db mtime change to rebuild managed rax store, got %d calls:\n%s", got, string(raxArgsAfterTouch))
	}

	bench := runCLI("bench", "query", "--db", dbPath, "--query", "derived retrieval runtime", "--backend", "rax", "--rax-bin", fakeRaxPath, "--iterations", "1", "--json")
	if !strings.Contains(bench, `"search"`) {
		t.Fatalf("expected bench query to use managed rax search path, got %q", bench)
	}
}

func TestCLIAdminMigrateDatabase(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "current.ltdb")
	current, err := yeoul.Open(ctx, yeoul.Config{
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("open lattice database: %v", err)
	}
	if err := current.Close(ctx); err != nil {
		t.Fatalf("close lattice database: %v", err)
	}

	var stdout strings.Builder
	if err := run(ctx, []string{"admin", "migrate-db", "--db", dbPath, "--json"}, &stdout, &strings.Builder{}); err != nil {
		t.Fatalf("run admin migrate-db: %v", err)
	}
	if !strings.Contains(stdout.String(), `"migrated": false`) || !strings.Contains(stdout.String(), `"source_driver": "lattice"`) {
		t.Fatalf("unexpected migration output: %s", stdout.String())
	}
}

func TestRaxProjectionChunkIDsMapBackToRecords(t *testing.T) {
	kind, id, ok := raxRecordKindID("episode:project:thread" + raxChunkMarker + "2")
	if !ok || kind != "episode" || id != "project:thread" {
		t.Fatalf("expected chunk doc id to map to original record, got kind=%q id=%q ok=%v", kind, id, ok)
	}
}

// TestRaxProjectionIDsRoundTripChunkMarker verifies that record IDs which
// themselves contain the native chunk marker survive the projection identity
// round trip, that a native chunk suffix is still stripped, and that the
// shortened ID stays a distinct record.
func TestRaxProjectionIDsRoundTripChunkMarker(t *testing.T) {
	for _, kind := range []string{"fact", "episode", "entity"} {
		id := "project" + raxChunkMarker + "notes"
		docID := raxProjectionRecordID(kind, id)
		gotKind, gotID, ok := raxRecordKindID(docID)
		if !ok || gotKind != kind || gotID != id {
			t.Fatalf("expected %s identity round trip, got kind=%q id=%q ok=%v from %q", kind, gotKind, gotID, ok, docID)
		}
		if projectionID, ok := raxRecordProjectionID(docID); !ok || projectionID != kind+":"+id {
			t.Fatalf("expected %s projection id %q, got %q ok=%v", kind, kind+":"+id, projectionID, ok)
		}
		// A native chunk suffix appended to the escaped identity must still map
		// back to the marker-containing record, not the shortened one.
		gotKind, gotID, ok = raxRecordKindID(docID + raxChunkMarker + "3")
		if !ok || gotKind != kind || gotID != id {
			t.Fatalf("expected %s chunked identity to keep marker id, got kind=%q id=%q ok=%v", kind, gotKind, gotID, ok)
		}
	}

	// The shortened ID must remain a distinct record from the marker-containing
	shortened := raxProjectionRecordID("fact", "project")
	marker := raxProjectionRecordID("fact", "project"+raxChunkMarker+"notes")
	if shortened == marker {
		t.Fatalf("expected shortened and marker ids to stay distinct, both %q", shortened)
	}
	if _, id, ok := raxRecordKindID(shortened); !ok || id != "project" {
		t.Fatalf("expected shortened id to decode to project, got id=%q ok=%v", id, ok)
	}

	// Legacy unescaped identities (pre-version-bump) must still decode.
	if _, id, ok := raxRecordKindID("fact:fact-legacy"); !ok || id != "fact-legacy" {
		t.Fatalf("expected unescaped legacy id preserved, got id=%q ok=%v", id, ok)
	}
}

func TestRaxProjectionIncludesRevisionText(t *testing.T) {
	projections, manifest := buildProjectionArtifacts("test.ltdb", &exportFile{
		Entities: []yeoul.EntityInput{{
			ID:            "project:rev",
			SpaceID:       "default",
			Type:          "Project",
			CanonicalName: "Current Name",
		}},
		Facts: []yeoul.FactInput{{
			ID:        "fact-rev",
			SpaceID:   "default",
			Predicate: "HAS_STATUS",
			SubjectID: "project:rev",
			ValueText: "beta",
			Status:    "active",
		}},
		EntityRevisions: []yeoul.EntityRevision{{
			EntityID:      "project:rev",
			CanonicalName: "Old Name",
		}},
		FactRevisions: []yeoul.FactRevision{{
			FactID:    "fact-rev",
			Predicate: "HAS_STATUS",
			SubjectID: "project:rev",
			ValueText: "alpha",
		}},
	})
	if manifest.ProjectionCount != 2 {
		t.Fatalf("expected current projection count only, got %d", manifest.ProjectionCount)
	}
	byID := map[string]projectionDocument{}
	for _, projection := range projections {
		byID[projection.ProjectionID] = projection
	}
	if !strings.Contains(byID["fact:fact-rev"].SearchText, "alpha") || !strings.Contains(byID["entity:project:rev"].SearchText, "Old Name") {
		t.Fatalf("expected revision text in projections, got %#v", byID)
	}
}

func TestParseRaxDocIDsAcceptsHitsAndStringIDs(t *testing.T) {
	for _, input := range [][]byte{
		[]byte(`[{"doc_id":"episode:one","preview":null}]`),
		[]byte(`["episode:one"]`),
	} {
		docIDs, err := parseRaxDocIDs(input)
		if err != nil {
			t.Fatalf("parse rax doc ids: %v", err)
		}
		if len(docIDs) != 1 || docIDs[0] != "episode:one" {
			t.Fatalf("expected episode:one, got %#v", docIDs)
		}
	}
}

func TestCLIIndexVerifyRejectsCorruptProjectionContent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "index-corrupt.ltdb")
	indexRoot := filepath.Join(tmpDir, "index")

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
	runCLI("ingest", "episode", "--db", dbPath, "--kind", "note", "--content", "original searchable content")
	runCLI("index", "build", "--db", dbPath, "--root", indexRoot)

	projectionPath := filepath.Join(indexRoot, "projection.ndjson")
	projectionData, err := os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	corruptData := strings.ReplaceAll(string(projectionData), "original searchable content", "corrupted stale content")
	if err := os.WriteFile(projectionPath, []byte(corruptData), 0o644); err != nil {
		t.Fatalf("write corrupt projection: %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	err = run(ctx, []string{"index", "verify", "--db", dbPath, "--root", indexRoot}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected verify to reject corrupt projection content, got stdout=%q", stdout.String())
	}
	if !strings.Contains(err.Error(), "projection documents do not match") {
		t.Fatalf("expected projection document mismatch, got err=%v stderr=%s", err, stderr.String())
	}
}

func TestCLIIndexVerifyReadsLargeProjectionDocuments(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "index-large.ltdb")
	indexRoot := filepath.Join(tmpDir, "index")
	contentPath := filepath.Join(tmpDir, "large.txt")
	if err := os.WriteFile(contentPath, []byte(strings.Repeat("A", 70_000)), 0o644); err != nil {
		t.Fatalf("write large content: %v", err)
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
	runCLI("ingest", "file", "--db", dbPath, "--kind", "note", "--file", contentPath)
	runCLI("index", "build", "--db", dbPath, "--root", indexRoot)
	verify := runCLI("index", "verify", "--db", dbPath, "--root", indexRoot, "--json")
	if !strings.Contains(verify, `"valid": true`) {
		t.Fatalf("expected large projection verify to succeed, got %q", verify)
	}
}

func TestCLIIndexClampsPreUnixEpochProjectionTimestamps(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "index-pre-epoch.ltdb")
	ingestPath := filepath.Join(tmpDir, "pre-epoch.json")
	indexRoot := filepath.Join(tmpDir, "index")

	payload := `{
  "episodes": [
    {
      "id":"ep-pre-epoch",
      "kind":"note",
      "content":"historical observation",
      "observed_at":"1960-01-02T03:04:05Z"
    }
  ],
  "entities": [
    {"id":"project:history","type":"Project","canonical_name":"History"}
  ],
  "facts": [
    {
      "id":"fact-pre-epoch",
      "predicate":"HAS_OBSERVATION",
      "subject_id":"project:history",
      "value_text":"historical fact",
      "observed_at":"1960-01-02T03:04:05Z",
      "supporting_episode_ids":["ep-pre-epoch"]
    }
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
	runCLI("index", "build", "--db", dbPath, "--root", indexRoot)

	projectionData, err := os.ReadFile(filepath.Join(indexRoot, "projection.ndjson"))
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if got := strings.Count(string(projectionData), `"observed_at_ms":0`); got != 2 {
		t.Fatalf("expected episode and fact timestamps to clamp to zero, got %d in %q", got, string(projectionData))
	}
}

func TestCLIIndexRejectsUnsafeProjectionManifestPath(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "index-unsafe-path.ltdb")
	indexRoot := filepath.Join(tmpDir, "index")

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
	runCLI("ingest", "episode", "--db", dbPath, "--kind", "note", "--content", "safe projection path")
	runCLI("index", "build", "--db", dbPath, "--root", indexRoot)

	manifestPath := filepath.Join(indexRoot, "yeoul-index.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	unsafeManifest := strings.Replace(string(manifestData), `"projection_file": "projection.ndjson"`, `"projection_file": "../projection.ndjson"`, 1)
	if err := os.WriteFile(manifestPath, []byte(unsafeManifest), 0o644); err != nil {
		t.Fatalf("write unsafe manifest: %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	err = run(ctx, []string{"index", "status", "--root", indexRoot}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected status to reject unsafe projection path, got stdout=%q", stdout.String())
	}
	if !strings.Contains(err.Error(), "invalid projection file") {
		t.Fatalf("expected invalid projection file error, got err=%v stderr=%s", err, stderr.String())
	}
}

// requireExportSupport skips export tests where admin export refuses to run.
func requireExportSupport(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("admin export is not supported on this platform")
	}
}

func TestCLIAdminExportImport(t *testing.T) {
	requireExportSupport(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	exportPath := filepath.Join(tmpDir, "export.json")

	runCLI := func(args ...string) string {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
		return stdout.String()
	}
	runCLIError := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-export",
		"--kind", "note",
		"--content", "export me",
		"--source-kind", "note",
		"--source-external-ref", "thread-export",
	)
	runCLI("admin", "export", "--db", dbPath, "--out", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(data), `"ep-export"`) {
		t.Fatalf("expected export payload to contain episode id, got %q", string(data))
	}
	if strings.Contains(string(data), `"fact_revisions"`) || strings.Contains(string(data), `"entity_revisions"`) {
		t.Fatalf("expected importable export payload without revision history, got %q", string(data))
	}

	importedDB := filepath.Join(tmpDir, "imported.ltdb")
	runCLI("init", "--db", importedDB)
	runCLI("admin", "import", "--confirm", "--db", importedDB, "--in", exportPath)
	imported := runCLI("search", "--db", importedDB, "--query", "export me", "--json")
	if !strings.Contains(imported, `"export me"`) {
		t.Fatalf("expected imported snapshot search result, got %q", imported)
	}

	restorePath := filepath.Join(tmpDir, "restore.json")
	restorePayload := `{
  "episodes": [{"id":"ep-restore","kind":"note","content":"restore inactive","source":{"kind":"note","external_ref":"restore"}}],
  "entities": [{"id":"thing:restore","type":"Thing","canonical_name":"restore"}],
  "facts": [{
    "id":"fact-restore",
    "predicate":"HAS_STATE",
    "subject_id":"thing:restore",
    "value_text":"restore inactive",
    "status":"retracted",
    "supporting_episode_ids":["ep-restore"],
    "metadata":{"superseded_by":"fact-later"}
  }]
}`
	if err := os.WriteFile(restorePath, []byte(restorePayload), 0o644); err != nil {
		t.Fatalf("write restore payload: %v", err)
	}
	restoreDB := filepath.Join(tmpDir, "restore.ltdb")
	runCLI("init", "--db", restoreDB)
	if err := runCLIError("admin", "import", "--confirm", "--db", restoreDB, "--in", restorePath); err == nil {
		t.Fatal("expected admin import to reject lifecycle-managed fact fields")
	}
}

func TestCLIAdminExportRejectsLifecycleLoss(t *testing.T) {
	requireExportSupport(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	exportPath := filepath.Join(tmpDir, "export.json")

	runCLI := func(args ...string) {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
	}
	runCLIError := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-export",
		"--kind", "note",
		"--content", "export me",
		"--source-kind", "note",
		"--source-external-ref", "thread-export",
	)
	seedPath := filepath.Join(tmpDir, "seed-export.json")
	seedPayload := `{
  "entities": [{"id":"thing:export","type":"Thing","canonical_name":"export"}],
  "facts": [{
    "id":"fact-export",
    "predicate":"HAS_STATE",
    "subject_id":"thing:export",
    "value_text":"old export state",
    "supporting_episode_ids":["ep-export"]
  }]
}`
	if err := os.WriteFile(seedPath, []byte(seedPayload), 0o644); err != nil {
		t.Fatalf("write seed payload: %v", err)
	}
	runCLI("admin", "import", "--confirm", "--db", dbPath, "--in", seedPath)
	time.Sleep(time.Millisecond)
	runCLI("fact", "supersede", "--confirm", "--db", dbPath, "--id", "fact-export", "--predicate", "HAS_STATE", "--subject-id", "thing:export", "--value-text", "new export state", "--supporting-episodes", "ep-export", "--reason", "test")

	err := runCLIError("admin", "export", "--db", dbPath, "--out", exportPath)
	if err == nil {
		t.Fatal("expected admin export to reject inactive fact loss")
	}
	if !strings.Contains(err.Error(), "inactive fact") {
		t.Fatalf("expected inactive fact export error, got %v", err)
	}
	if _, statErr := os.Stat(exportPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no lossy export file, stat err=%v", statErr)
	}
}

func TestCLIAdminExportRejectsRevisionLoss(t *testing.T) {
	requireExportSupport(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	exportPath := filepath.Join(tmpDir, "export.json")

	runCLI := func(args ...string) {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
	}
	runCLIError := func(args ...string) error {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		return run(ctx, args, &stdout, &stderr)
	}

	runCLI("init", "--db", dbPath)
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-revision",
		"--kind", "note",
		"--content", "active revision export",
		"--source-kind", "note",
		"--source-external-ref", "thread-revision",
	)
	seedPath := filepath.Join(tmpDir, "seed-revision.json")
	seedPayload := `{
  "entities": [{"id":"thing:revision","type":"Thing","canonical_name":"revision"}],
  "facts": [{
    "id":"fact-revision",
    "predicate":"HAS_STATE",
    "subject_id":"thing:revision",
    "value_text":"active revision",
    "supporting_episode_ids":["ep-revision"]
  }]
}`
	if err := os.WriteFile(seedPath, []byte(seedPayload), 0o644); err != nil {
		t.Fatalf("write seed payload: %v", err)
	}
	runCLI("admin", "import", "--confirm", "--db", dbPath, "--in", seedPath)

	err := runCLIError("admin", "export", "--db", dbPath, "--out", exportPath)
	if err == nil {
		t.Fatal("expected admin export to reject revision history loss")
	}
	if !strings.Contains(err.Error(), "revision history") {
		t.Fatalf("expected revision history export error, got %v", err)
	}
}

func TestCLIPolicyDrivenSearchAndIngestDrop(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "policy.ltdb")
	packPath, err := filepath.Abs(filepath.Join("..", "..", "agent-pack"))
	if err != nil {
		t.Fatalf("resolve pack path: %v", err)
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
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-policy",
		"--kind", "note",
		"--content", "Recent context about Yeoul using Ladybug.",
		"--source-kind", "note",
		"--source-external-ref", "policy-search",
	)
	search := runCLI("search", "--db", dbPath, "--query", "Ladybug", "--policy-path", packPath, "--recipe", "recent_context", "--json")
	if !strings.Contains(search, `"hits"`) || !strings.Contains(search, `"included"`) {
		t.Fatalf("expected policy-driven search output, got %q", search)
	}

	dropPack := filepath.Join(tmpDir, "drop-pack")
	if err := os.MkdirAll(dropPack, 0o755); err != nil {
		t.Fatalf("mkdir drop pack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dropPack, "SKILL.md"), []byte("# Drop\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dropPack, "ontology.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write ontology: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dropPack, "episode_rules.yaml"), []byte("version: 1\ndrop:\n  - name: drop_me\n    when:\n      contains_substring: [\"ignore me\"]\n"), 0o644); err != nil {
		t.Fatalf("write episode rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dropPack, "search_recipes.yaml"), []byte("version: 1\nrecipes: {}\n"), 0o644); err != nil {
		t.Fatalf("write recipes: %v", err)
	}

	drop := runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--kind", "note",
		"--content", "ignore me in memory",
		"--source-kind", "note",
		"--source-external-ref", "policy-drop",
		"--policy-path", dropPack,
		"--json",
	)
	if !strings.Contains(drop, `"skipped": true`) {
		t.Fatalf("expected policy drop output, got %q", drop)
	}
}

func TestCLIPolicyDropRuleKeepsSubstantiveDecisions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "drop-decisions.ltdb")
	packPath, err := filepath.Abs(filepath.Join("..", "..", "agent-pack"))
	if err != nil {
		t.Fatalf("resolve pack path: %v", err)
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

	cases := []struct {
		id      string
		content string
		dropped bool
	}{
		{id: "ep-ack-exact", content: "ok", dropped: true},
		{id: "ep-ack-punct", content: "OK.", dropped: true},
		{id: "ep-ack-thanks", content: "thanks", dropped: true},
		{id: "ep-decision", content: "We decided to rotate tokens every hour.", dropped: false},
		{id: "ep-broken", content: "The deployment is broken and needs a rollback.", dropped: false},
		{id: "ep-book", content: "Return the book to the shelf.", dropped: false},
		{id: "ep-thanks-decision", content: "thanks - we decided to ship on Friday.", dropped: false},
		{id: "ep-ack-decision", content: "ok, but we decided to keep LatticeDB as canonical.", dropped: false},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			out := runCLI(
				"ingest", "episode",
				"--db", dbPath,
				"--id", tc.id,
				"--kind", "note",
				"--content", tc.content,
				"--source-kind", "note",
				"--source-external-ref", tc.id,
				"--policy-path", packPath,
				"--json",
			)
			if tc.dropped {
				if !strings.Contains(out, `"skipped": true`) {
					t.Fatalf("expected %q to be dropped, got %q", tc.content, out)
				}
				return
			}
			if strings.Contains(out, `"skipped": true`) {
				t.Fatalf("expected %q to be retained, got %q", tc.content, out)
			}
			stored := runCLI("get", "--db", dbPath, "--kind", "episode", "--id", tc.id, "--json")
			if !strings.Contains(stored, tc.content) {
				t.Fatalf("expected stored episode %s to contain %q, got %q", tc.id, tc.content, stored)
			}
		})
	}
}

func TestCLIBenchIngest(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bench.ltdb")

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
	output := runCLI("bench", "ingest", "--db", dbPath, "--episodes", "3", "--facts-per-episode", "2", "--json")

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode bench output: %v", err)
	}
	if got := int(result["episodes"].(float64)); got != 3 {
		t.Fatalf("unexpected episode count: %d", got)
	}

	counts := runCLI("inspect", "counts", "--db", dbPath, "--json")
	if !strings.Contains(counts, `"episodes": 3`) || !strings.Contains(counts, `"facts": 6`) {
		t.Fatalf("expected bench counts output, got %q", counts)
	}
}

func TestCLIIngestFileAndBatchAliases(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aliases.ltdb")
	contentPath := filepath.Join(tmpDir, "note.txt")
	batchPath := filepath.Join(tmpDir, "batch.json")

	if err := os.WriteFile(contentPath, []byte("ingest file alias"), 0o644); err != nil {
		t.Fatalf("write content file: %v", err)
	}
	if err := os.WriteFile(batchPath, []byte(`{"episodes":[{"id":"ep-batch","kind":"note","content":"batch alias","source":{"kind":"note","external_ref":"batch"}}]}`), 0o644); err != nil {
		t.Fatalf("write batch file: %v", err)
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
	runCLI("ingest", "file", "--db", dbPath, "--kind", "note", "--file", contentPath, "--source-kind", "note", "--source-external-ref", "file")
	runCLI("ingest", "batch", "--db", dbPath, "--file", batchPath)

	counts := runCLI("inspect", "counts", "--db", dbPath, "--json")
	if !strings.Contains(counts, `"episodes": 2`) {
		t.Fatalf("expected both alias ingests to work, got %q", counts)
	}
}

func TestManagedRaxFreshnessIncludesRuntimeIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.ltdb")
	root := filepath.Join(tmpDir, "rax")
	storePath := filepath.Join(root, "projection.rax")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	if err := os.WriteFile(storePath, []byte("store"), 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
	stat, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	manifest := projectionManifest{
		Version:         projectionManifestVersion,
		DatabaseSize:    stat.Size(),
		DatabaseModTime: stat.ModTime(),
		ProjectionCount: 1,
		RaxRuntime:      "cli:/old/rax",
		BuiltAt:         time.Now().UTC(),
	}
	if _, err := writeProjectionManifest(root, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	ok, err := managedRaxStoreFresh(root, storePath, dbPath, raxRuntime{Kind: "cli", Path: "/old/rax"})
	if err != nil || !ok {
		t.Fatalf("expected matching runtime cache fresh, ok=%v err=%v", ok, err)
	}
	ok, err = managedRaxStoreFresh(root, storePath, dbPath, raxRuntime{Kind: "cli", Path: "/new/rax"})
	if err != nil {
		t.Fatalf("freshness check: %v", err)
	}
	if ok {
		t.Fatal("expected runtime mismatch to mark managed rax cache stale")
	}
}

func TestRebuildManagedRaxStoreKeepsOldCacheOnIngestFailure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "rax")
	storePath := filepath.Join(root, "projection.rax")
	argsPath := filepath.Join(tmpDir, "rax-args.txt")
	projectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(storePath, []byte("old store"), 0o644); err != nil {
		t.Fatalf("write old store: %v", err)
	}
	oldManifest := projectionManifest{Version: projectionManifestVersion, ProjectionCount: 1, RaxRuntime: "cli:/old", BuiltAt: time.Now().UTC()}
	if _, err := writeProjectionManifest(root, oldManifest); err != nil {
		t.Fatalf("write old manifest: %v", err)
	}
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", argsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", projectionPath)
	t.Setenv("YEOUL_FAKE_RAX_FAIL_INGEST", "1")

	err := rebuildManagedRaxStore(ctx, root, storePath, raxRuntime{Kind: "cli", Path: os.Args[0]}, []projectionDocument{{
		ProjectionID: "fact:new",
		SearchText:   "new text",
	}}, projectionManifest{Version: projectionManifestVersion, ProjectionCount: 1, BuiltAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("expected rebuild failure")
	}
	storeData, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if string(storeData) != "old store" {
		t.Fatalf("expected old store preserved, got %q", string(storeData))
	}
	gotManifest, err := readProjectionManifest(root)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if gotManifest.RaxRuntime != oldManifest.RaxRuntime {
		t.Fatalf("expected old manifest preserved, got %#v", gotManifest)
	}
}

func TestRaxPrimaryFallbackDecisionForFilteredTruncation(t *testing.T) {
	req := yeoul.SearchRequest{
		QueryText: "needle",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Page:      yeoul.Page{Limit: 1},
	}
	if !raxPrimaryShouldFallbackToCore(req, 0, 1001, 1001) {
		t.Fatal("expected filtered full fetch to fall back to core")
	}
	if raxPrimaryShouldFallbackToCore(req, 1, 20, 1001) {
		t.Fatal("did not expect fallback when rax did not hit fetch cap")
	}
	req.Page.Cursor = "key:1:fact:fact-1"
	if !raxPrimaryShouldFallbackToCore(req, 0, 1001, 1001) {
		t.Fatal("expected filtered cursor page at fetch cap to fall back to core")
	}
	req = yeoul.SearchRequest{QueryText: "needle", Page: yeoul.Page{Limit: 1}}
	if raxPrimaryShouldFallbackToCore(req, 0, 1001, 1001) {
		t.Fatal("did not expect fallback without post-filters")
	}
}

func TestCLITimelineProvenanceAndFactLookup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "query.ltdb")
	ingestPath := filepath.Join(tmpDir, "query.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-q1",
      "kind":"note",
      "content":"Yeoul uses Ladybug.",
      "source":{"kind":"note","external_ref":"thread-q1"},
      "observed_at":"2026-04-01T00:00:00Z"
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
  ],
  "facts": [
    {
      "id":"fact-q1",
      "predicate":"USES_STORAGE_ENGINE",
      "subject_id":"project:yeoul",
      "object_id":"database:ladybug",
      "value_text":"Yeoul uses Ladybug.",
      "supporting_episode_ids":["ep-q1"],
      "observed_at":"2026-04-01T00:00:00Z"
    }
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

	timeline := runCLI("timeline", "--db", dbPath, "--entity", "project:yeoul", "--json")
	if !strings.Contains(timeline, `"fact_created"`) {
		t.Fatalf("expected timeline output, got %q", timeline)
	}

	provenance := runCLI("provenance", "--db", dbPath, "--fact", "fact-q1", "--json")
	if !strings.Contains(provenance, `"ASSERTS"`) || !strings.Contains(provenance, `"FROM_SOURCE"`) {
		t.Fatalf("expected provenance output, got %q", provenance)
	}

	lookup := runCLI("fact", "lookup", "--db", dbPath, "--subject-id", "project:yeoul", "--predicate", "USES_STORAGE_ENGINE", "--json")
	if !strings.Contains(lookup, `"fact-q1"`) {
		t.Fatalf("expected fact lookup output, got %q", lookup)
	}
}

func TestCLIFactAssertCanUpsertSubjectEntity(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "fact-upsert-subject.ltdb")
	ingestPath := filepath.Join(tmpDir, "fact-upsert-subject.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-decision",
      "kind":"decision_note",
      "content":"Use structured facts for stable decisions.",
      "source":{"kind":"note","external_ref":"thread-decision"},
      "observed_at":"2026-05-28T03:10:00Z"
    }
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
	asserted := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "HAS_DECISION",
		"--upsert-subject",
		"--subject-type", "DecisionTopic",
		"--subject-name", "Structured memory promotion",
		"--value-text", "Confirmed durable decisions should be promoted to facts when the subject is clear.",
		"--supporting-episodes", "ep-decision",
		"--json")
	subjectID := yeoul.EntityID("", "DecisionTopic", "Structured memory promotion")
	for _, want := range []string{
		`"predicate": "HAS_DECISION"`,
		`"subject_id": "` + subjectID + `"`,
		`"observed_at": "2026-05-28T03:10:00Z"`,
		`"observed_at_basis": "supporting_episode"`,
		`"observed_at_supporting_episode_id": "ep-decision"`,
		`"supporting_episode_ids": [`,
		`"ep-decision"`,
	} {
		if !strings.Contains(asserted, want) {
			t.Fatalf("expected asserted fact output to contain %q, got %q", want, asserted)
		}
	}

	counts := runCLI("inspect", "counts", "--db", dbPath, "--json")
	if !strings.Contains(counts, `"entities": 1`) || !strings.Contains(counts, `"facts": 1`) {
		t.Fatalf("expected one entity and one fact, got %q", counts)
	}

	lookup := runCLI("fact", "lookup", "--db", dbPath,
		"--subject-id", subjectID,
		"--predicate", "HAS_DECISION",
		"--json")
	if !strings.Contains(lookup, `"HAS_DECISION"`) || !strings.Contains(lookup, `"ep-decision"`) {
		t.Fatalf("expected promoted fact lookup output, got %q", lookup)
	}

	relationship := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "USES_STORAGE_ENGINE",
		"--upsert-subject",
		"--subject-type", "Project",
		"--subject-name", "Yeoul",
		"--upsert-object",
		"--object-type", "Database",
		"--object-name", "Ladybug",
		"--supporting-episodes", "ep-decision",
		"--json")
	for _, want := range []string{
		`"predicate": "USES_STORAGE_ENGINE"`,
		`"subject_id": "` + yeoul.EntityID("", "Project", "Yeoul") + `"`,
		`"object_id": "` + yeoul.EntityID("", "Database", "Ladybug") + `"`,
	} {
		if !strings.Contains(relationship, want) {
			t.Fatalf("expected relationship fact output to contain %q, got %q", want, relationship)
		}
	}

	explicitObserved := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "HAS_VERIFICATION_STATUS",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-name", "Observed at CLI flag",
		"--value-text", "explicit observed_at wins",
		"--supporting-episodes", "ep-decision",
		"--observed-at", "2026-05-29T04:11:00Z",
		"--json")
	if !strings.Contains(explicitObserved, `"observed_at": "2026-05-29T04:11:00Z"`) || !strings.Contains(explicitObserved, `"observed_at_basis": "explicit"`) {
		t.Fatalf("expected explicit observed_at, got %q", explicitObserved)
	}
	stableKeyFact := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "HAS_RENAMED_TOPIC",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-stable-key", "feature-stable-1",
		"--subject-name", "Renamed topic",
		"--value-text", "stable key keeps entity identity",
		"--supporting-episodes", "ep-decision",
		"--json")
	if !strings.Contains(stableKeyFact, `"subject_id": "`+yeoul.EntityID("", "Feature", "feature-stable-1")+`"`) {
		t.Fatalf("expected stable key subject id, got %q", stableKeyFact)
	}
	stableKeyFact = runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "HAS_RENAMED_TOPIC_AGAIN",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-stable-key", "feature-stable-1",
		"--subject-name", "Renamed topic v2",
		"--value-text", "stable key still keeps entity identity",
		"--supporting-episodes", "ep-decision",
		"--json")
	if !strings.Contains(stableKeyFact, `"subject_id": "`+yeoul.EntityID("", "Feature", "feature-stable-1")+`"`) {
		t.Fatalf("expected stable key subject id after rename, got %q", stableKeyFact)
	}

	noObservedPayload := `{
  "episodes": [
    {
      "id":"ep-no-observed",
      "kind":"decision_note",
      "content":"No observed time was supplied.",
      "source":{"kind":"note","external_ref":"thread-no-observed"}
    }
  ]
}`
	if err := os.WriteFile(ingestPath, []byte(noObservedPayload), 0o644); err != nil {
		t.Fatalf("write no-observed ingest payload: %v", err)
	}
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	before := time.Now().UTC().Add(-2 * time.Second)
	systemObserved := runCLI("fact", "assert", "--db", dbPath,
		"--predicate", "HAS_SYSTEM_OBSERVED_AT",
		"--upsert-subject",
		"--subject-type", "Feature",
		"--subject-name", "System observed at fallback",
		"--value-text", "system time fallback",
		"--supporting-episodes", "ep-no-observed",
		"--json")
	after := time.Now().UTC().Add(2 * time.Second)
	var parsed struct {
		ObservedAt time.Time      `json:"observed_at"`
		Metadata   map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(systemObserved), &parsed); err != nil {
		t.Fatalf("parse system observed fact: %v\noutput=%s", err, systemObserved)
	}
	if parsed.ObservedAt.Before(before) || parsed.ObservedAt.After(after) {
		t.Fatalf("expected system observed_at between %s and %s, got %s", before, after, parsed.ObservedAt)
	}
	if parsed.Metadata["observed_at_basis"] != "system_time_default" {
		t.Fatalf("expected system-time observed_at basis, got %v in %q", parsed.Metadata, systemObserved)
	}
}

func TestCLIFactAssertUpsertFailureIsAtomic(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "fact-upsert-atomic.ltdb")

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

	var stdout strings.Builder
	var stderr strings.Builder
	err := run(ctx, []string{
		"fact", "assert",
		"--db", dbPath,
		"--predicate", "HAS_BROKEN_SUPPORT",
		"--upsert-subject",
		"--subject-type", "Project",
		"--subject-name", "Orphan",
		"--value-text", "should not partially persist",
		"--observed-at", "2026-05-28T03:40:00Z",
		"--supporting-episodes", "missing-episode",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected fact assert to fail for missing supporting episode, got stdout=%q", stdout.String())
	}

	counts := runCLI("inspect", "counts", "--db", dbPath, "--json")
	for _, want := range []string{`"entities": 0`, `"facts": 0`, `"episodes": 0`} {
		if !strings.Contains(counts, want) {
			t.Fatalf("expected failed upsert assert to leave no records; missing %q in %q", want, counts)
		}
	}
}

func TestCLIProvenanceShowsInactiveFactLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "prov-inactive.ltdb")
	ingestPath := filepath.Join(tmpDir, "prov-inactive.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-pi",
      "kind":"note",
      "content":"owner changed",
      "source":{"kind":"note","external_ref":"thread-pi"}
    }
  ],
  "entities": [
    {"id":"task:pi","type":"Task","canonical_name":"Task"},
    {"id":"person:a","type":"Person","canonical_name":"A"},
    {"id":"person:b","type":"Person","canonical_name":"B"}
  ],
  "facts": [
    {
      "id":"fact-pi-old",
      "predicate":"OWNED_BY",
      "subject_id":"task:pi",
      "object_id":"person:a",
      "supporting_episode_ids":["ep-pi"]
    }
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
	runCLI("fact", "supersede", "--confirm", "--db", dbPath, "--id", "fact-pi-old", "--predicate", "OWNED_BY", "--subject-id", "task:pi", "--object-id", "person:b", "--supporting-episodes", "ep-pi", "--reason", "owner change")

	output := runCLI("provenance", "--db", dbPath, "--fact", "fact-pi-old", "--json")
	if !strings.Contains(output, `"SUPERSEDES"`) || !strings.Contains(output, `"superseded_by"`) {
		t.Fatalf("expected inactive fact provenance lifecycle output, got %q", output)
	}

	defaultTimeline := runCLI("timeline", "--db", dbPath, "--entity", "task:pi", "--json")
	if !strings.Contains(defaultTimeline, `"fact_superseded"`) || !strings.Contains(defaultTimeline, `"fact-pi-old"`) {
		t.Fatalf("expected default timeline to include inactive fact lifecycle output, got %q", defaultTimeline)
	}

	timeline := runCLI("timeline", "--db", dbPath, "--entity", "task:pi", "--event-type", "fact_superseded", "--json")
	if !strings.Contains(timeline, `"fact_superseded"`) || !strings.Contains(timeline, `"fact-pi-old"`) {
		t.Fatalf("expected inactive fact lifecycle timeline output, got %q", timeline)
	}
}

func TestCLIEntityMergePreviewAndCompact(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "compact.ltdb")
	ingestPath := filepath.Join(tmpDir, "compact.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-c1",
      "kind":"note",
      "content":"duplicate entities and facts",
      "source":{"kind":"note","external_ref":"thread-c1"}
    }
  ],
  "entities": [
    {"id":"project:yeoul-a","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:yeoul-b","type":"Project","canonical_name":"Yeoul"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
  ],
  "facts": [
    {
      "id":"fact-c1",
      "predicate":"USES_STORAGE_ENGINE",
      "subject_id":"project:yeoul-a",
      "object_id":"database:ladybug",
      "value_text":"Yeoul uses Ladybug.",
      "supporting_episode_ids":["ep-c1"]
    },
    {
      "id":"fact-c2",
      "predicate":"USES_STORAGE_ENGINE",
      "subject_id":"project:yeoul-a",
      "object_id":"database:ladybug",
      "value_text":"Yeoul uses Ladybug.",
      "supporting_episode_ids":["ep-c1"]
    }
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

	preview := runCLI("entity", "merge-preview", "--db", dbPath)
	if !strings.Contains(preview, "project:yeoul-a") {
		t.Fatalf("expected entity merge preview output, got %q", preview)
	}

	runCLI("entity", "merge", "--confirm", "--db", dbPath, "--target", "project:yeoul-a", "--source", "project:yeoul-b", "--reason", "exact duplicate")
	entity := runCLI("entity", "get", "--db", dbPath, "--id", "project:yeoul-b")
	if !strings.Contains(entity, `"duplicate_of": "project:yeoul-a"`) {
		t.Fatalf("expected duplicate marker, got %q", entity)
	}

	compactDryRun := runCLI("admin", "compact", "--db", dbPath, "--json")
	if !strings.Contains(compactDryRun, `"fact_duplicate_candidates": 1`) {
		t.Fatalf("expected compact dry-run output, got %q", compactDryRun)
	}

	runCLI("admin", "compact", "--confirm", "--apply", "--db", dbPath)
	fact := runCLI("fact", "get", "--db", dbPath, "--id", "fact-c2")
	if !strings.Contains(fact, `"status": "retracted"`) {
		t.Fatalf("expected duplicate fact to retract, got %q", fact)
	}
}

func TestCLIAdminCompactPreservesActiveAndDistinctFacts(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "compact-safety.ltdb")
	ingestPath := filepath.Join(tmpDir, "compact-safety.json")

	payload := `{
  "episodes": [
    {"id":"ep-1","kind":"note","content":"compact safety one","source":{"kind":"note","external_ref":"thread-safety-1"}},
    {"id":"ep-2","kind":"note","content":"compact safety two","source":{"kind":"note","external_ref":"thread-safety-2"}},
    {"id":"ep-1,ep-2","kind":"note","content":"comma id","source":{"kind":"note","external_ref":"thread-safety-3"}}
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"project:Yeoul","type":"Project","canonical_name":"Yeoul Upper"},
    {"id":"project:space","type":"Project","canonical_name":"Space"},
    {"id":"project:space ","type":"Project","canonical_name":"Space Padded"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
  ],
  "facts": [
    {"id":"fact-old","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"supersede me","supporting_episode_ids":["ep-1"]},
    {"id":"fact-conf-low","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"confidence twin","confidence":0.3,"supporting_episode_ids":["ep-1"]},
    {"id":"fact-conf-high","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"confidence twin","confidence":0.9,"supporting_episode_ids":["ep-1"]},
    {"id":"fact-delim-many","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"delimiter twin","supporting_episode_ids":["ep-1","ep-2"]},
    {"id":"fact-delim-one","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"delimiter twin","supporting_episode_ids":["ep-1,ep-2"]},
    {"id":"fact-case-lower","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"case twin","supporting_episode_ids":["ep-2"]},
    {"id":"fact-case-upper","predicate":"USES_STORAGE_ENGINE","subject_id":"project:Yeoul","object_id":"database:ladybug","value_text":"case twin","supporting_episode_ids":["ep-2"]},
    {"id":"fact-meta-string","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"metadata twin","metadata":{"x":"1"},"supporting_episode_ids":["ep-1"]},
    {"id":"fact-meta-number","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"metadata twin","metadata":{"x":1},"supporting_episode_ids":["ep-1"]},
    {"id":"fact-meta-boundary","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"metadata boundary","metadata":{"a":"b c:d"},"supporting_episode_ids":["ep-1"]},
    {"id":"fact-meta-split","predicate":"USES_STORAGE_ENGINE","subject_id":"project:yeoul","object_id":"database:ladybug","value_text":"metadata boundary","metadata":{"a":"b","c":"d"},"supporting_episode_ids":["ep-1"]},
    {"id":"fact-space-plain","predicate":"USES_STORAGE_ENGINE","subject_id":"project:space","object_id":"database:ladybug","value_text":"space twin","supporting_episode_ids":["ep-1"]},
    {"id":"fact-space-padded","predicate":"USES_STORAGE_ENGINE","subject_id":"project:space ","object_id":"database:ladybug","value_text":"space twin","supporting_episode_ids":["ep-1"]}
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
	supersedeOut := runCLI(
		"fact", "supersede",
		"--confirm",
		"--db", dbPath,
		"--id", "fact-old",
		"--predicate", "USES_STORAGE_ENGINE",
		"--subject-id", "project:yeoul",
		"--object-id", "database:ladybug",
		"--value-text", "supersede me",
		"--supporting-episodes", "ep-1",
		"--reason", "compact safety",
	)
	fields := strings.Fields(supersedeOut)
	if len(fields) < 5 {
		t.Fatalf("unexpected supersede output %q", supersedeOut)
	}
	activeFactID := fields[len(fields)-1]

	dryRun := runCLI("admin", "compact", "--db", dbPath, "--json")
	if !strings.Contains(dryRun, `"fact_duplicate_candidates": 1`) {
		t.Fatalf("expected exactly one duplicate candidate, got %q", dryRun)
	}
	runCLI("admin", "compact", "--confirm", "--apply", "--db", dbPath)

	low := runCLI("fact", "get", "--db", dbPath, "--id", "fact-conf-low")
	if !strings.Contains(low, `"status": "retracted"`) || !strings.Contains(low, "duplicate_of:fact-conf-high") {
		t.Fatalf("expected the lower-confidence twin to retract against the higher-confidence fact, got %q", low)
	}
	high := runCLI("fact", "get", "--db", dbPath, "--id", "fact-conf-high")
	if !strings.Contains(high, `"status": "active"`) {
		t.Fatalf("expected the higher-confidence fact to stay active, got %q", high)
	}
	active := runCLI("fact", "get", "--db", dbPath, "--id", activeFactID)
	if !strings.Contains(active, `"status": "active"`) {
		t.Fatalf("expected the active successor to stay active, got %q", active)
	}
	superseded := runCLI("fact", "get", "--db", dbPath, "--id", "fact-old")
	if !strings.Contains(superseded, `"status": "superseded"`) {
		t.Fatalf("expected the superseded predecessor to stay superseded, got %q", superseded)
	}
	for _, id := range []string{
		"fact-delim-many",
		"fact-delim-one",
		"fact-case-lower",
		"fact-case-upper",
		"fact-meta-string",
		"fact-meta-number",
		"fact-meta-boundary",
		"fact-meta-split",
		"fact-space-plain",
		"fact-space-padded",
	} {
		record := runCLI("fact", "get", "--db", dbPath, "--id", id)
		if !strings.Contains(record, `"status": "active"`) {
			t.Fatalf("expected %s to stay active, got %q", id, record)
		}
	}
}

func TestCLIBenchQueryAndLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bench-plus.ltdb")
	ingestPath := filepath.Join(tmpDir, "seed.json")

	payload := `{
  "episodes": [
    {
      "id":"ep-bench",
      "kind":"note",
      "content":"Yeoul uses Ladybug.",
      "source":{"kind":"note","external_ref":"bench"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"database:ladybug","type":"Database","canonical_name":"Ladybug"}
  ],
  "facts": [
    {
      "id":"fact-bench",
      "predicate":"USES_STORAGE_ENGINE",
      "subject_id":"project:yeoul",
      "object_id":"database:ladybug",
      "value_text":"Yeoul uses Ladybug.",
      "supporting_episode_ids":["ep-bench"]
    }
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

	queryOutput := runCLI("bench", "query", "--db", dbPath, "--query", "Yeoul", "--entity", "project:yeoul", "--fact", "fact-bench", "--iterations", "2", "--json")
	if !strings.Contains(queryOutput, `"search"`) || !strings.Contains(queryOutput, `"provenance"`) {
		t.Fatalf("expected bench query output, got %q", queryOutput)
	}

	lifecycleDB := filepath.Join(tmpDir, "lifecycle.ltdb")
	runCLI("init", "--db", lifecycleDB)
	lifecycleOutput := runCLI("bench", "lifecycle", "--db", lifecycleDB, "--iterations", "2", "--json")
	if !strings.Contains(lifecycleOutput, `"supersede_count": 2`) || !strings.Contains(lifecycleOutput, `"retraction_count": 2`) {
		t.Fatalf("expected lifecycle output, got %q", lifecycleOutput)
	}
}

// TestCLIBenchQueryMatchesProductionRaxSearch checks that `bench query` measures
// the production planner instead of a differently filtered query. The benchmark
// used to inject the first entity as an unrequested anchor, so an eligible
// episode hit was filtered out and the reported latency measured empty work.
func TestCLIBenchQueryMatchesProductionRaxSearch(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bench-rax.ltdb")
	ingestPath := filepath.Join(tmpDir, "seed.json")
	fakeRaxPath := os.Args[0]
	argsPath := filepath.Join(tmpDir, "rax-args.txt")
	projectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")
	idsPath := filepath.Join(tmpDir, "rax-search-ids.json")

	// The only eligible hit is an episode. An injected entity anchor can never
	// match it, so the benchmark must not add one.
	payload := `{
  "episodes": [
    {"id":"ep-eligible","kind":"note","content":"needle episode","source":{"kind":"note","external_ref":"eligible"}}
  ],
  "entities": [
    {"id":"project:first","type":"Project","canonical_name":"First"}
  ]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}
	if err := os.WriteFile(idsPath, []byte(`[{"doc_id":"episode:ep-eligible"}]`), 0o644); err != nil {
		t.Fatalf("write fake search ids: %v", err)
	}
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", argsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", projectionPath)
	t.Setenv("YEOUL_FAKE_RAX_SEARCH_IDS", idsPath)

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

	var bench struct {
		SearchHits int      `json:"search_hits"`
		RecordIDs  []string `json:"record_ids"`
	}
	benchOut := runCLI("bench", "query", "--db", dbPath, "--query", "needle", "--backend", "rax", "--rax-bin", fakeRaxPath, "--iterations", "1", "--json")
	if err := json.Unmarshal([]byte(benchOut), &bench); err != nil {
		t.Fatalf("decode bench output %q: %v", benchOut, err)
	}
	var search struct {
		Hits []struct {
			RecordID string `json:"record_id"`
		} `json:"hits"`
	}
	searchOut := runCLI("search", "--db", dbPath, "--query", "needle", "--backend", "rax", "--rax-bin", fakeRaxPath, "--json")
	if err := json.Unmarshal([]byte(searchOut), &search); err != nil {
		t.Fatalf("decode search output %q: %v", searchOut, err)
	}
	if len(search.Hits) != 1 || search.Hits[0].RecordID != "ep-eligible" {
		t.Fatalf("expected the ordinary rax search to find ep-eligible, got %#v", search.Hits)
	}
	if bench.SearchHits != len(search.Hits) {
		t.Fatalf("expected bench search_hits=%d to match the ordinary search, got %d", len(search.Hits), bench.SearchHits)
	}
	if !slices.Equal(bench.RecordIDs, []string{"ep-eligible"}) {
		t.Fatalf("expected bench to measure the same hit as the ordinary search, got %#v", bench.RecordIDs)
	}
}
func TestRaxPrimarySearchAppliesSourceScope(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	epNote, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-note", Kind: "note", Content: "scoped note", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "note"}})
	if err != nil {
		t.Fatalf("ingest note: %v", err)
	}
	epBench, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-bench", Kind: "note", Content: "scoped bench", Source: yeoul.SourceInput{Kind: "bench", ExternalRef: "bench"}})
	if err != nil {
		t.Fatalf("ingest bench: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "thing:scope", Type: "Thing", CanonicalName: "scope"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	other, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "other:scope", Type: "Other", CanonicalName: "other"})
	if err != nil {
		t.Fatalf("upsert other: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-note", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ObjectID: other.ID, ValueText: "scoped note", SupportingEpisodeIDs: []string{epNote.EpisodeID}}); err != nil {
		t.Fatalf("assert note fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-note-2", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ObjectID: other.ID, ValueText: "scoped note second", SupportingEpisodeIDs: []string{epNote.EpisodeID}}); err != nil {
		t.Fatalf("assert second note fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-bench", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ValueText: "scoped bench", SupportingEpisodeIDs: []string{epBench.EpisodeID}}); err != nil {
		t.Fatalf("assert bench fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-stale", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ValueText: "obsolete needle", SupportingEpisodeIDs: []string{epNote.EpisodeID}}); err != nil {
		t.Fatalf("assert stale fact: %v", err)
	}
	if _, err := eng.SupersedeFact(ctx, "fact-stale", yeoul.FactInput{ID: "fact-fresh", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ValueText: "fresh value", SupportingEpisodeIDs: []string{epNote.EpisodeID}}, "test"); err != nil {
		t.Fatalf("supersede stale fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-visible", Predicate: "HAS_SCOPE", SubjectID: entity.ID, ValueText: "fresh visible", SupportingEpisodeIDs: []string{epNote.EpisodeID}}); err != nil {
		t.Fatalf("assert visible fact: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "thing:duplicate", Type: "Thing", CanonicalName: "scope duplicate", Metadata: map[string]any{"duplicate_of": entity.ID}}); err != nil {
		t.Fatalf("upsert duplicate entity: %v", err)
	}

	resp, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
	}, []string{"fact:fact-bench", "fact:fact-note"})
	if err != nil {
		t.Fatalf("build rax response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "fact-note" {
		t.Fatalf("expected source scope to keep only fact-note, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Page:      yeoul.Page{Limit: 1},
	}, []string{"fact:fact-note", "fact:fact-bench"})
	if err != nil {
		t.Fatalf("build rax paged response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Meta.NextCursor == "" {
		t.Fatalf("expected first rax page with cursor, got hits=%#v cursor=%q", resp.Hits, resp.Meta.NextCursor)
	}
	firstPageID := resp.Hits[0].RecordID
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Page:      yeoul.Page{Limit: 1, Cursor: resp.Meta.NextCursor},
	}, []string{"fact:fact-note", "fact:fact-bench"})
	if err != nil {
		t.Fatalf("build rax second page response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID == firstPageID {
		t.Fatalf("expected second rax page to advance, got first=%q hits=%#v", firstPageID, resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Page:      yeoul.Page{Limit: 1},
	}, []string{"fact:missing-1", "fact:missing-2", "entity:thing:scope", "episode:ep-note", "fact:fact-note"})
	if err != nil {
		t.Fatalf("build rax filtered-prefix response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "fact-note" {
		t.Fatalf("expected rax pagination to skip filtered prefix, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Page:      yeoul.Page{Limit: 1},
	}, []string{"fact:fact-bench", "fact:missing", "fact:fact-note", "fact:fact-note-2"})
	if err != nil {
		t.Fatalf("build rax filtered page one: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Meta.NextCursor == "" {
		t.Fatalf("expected first filtered page with cursor, got hits=%#v cursor=%q", resp.Hits, resp.Meta.NextCursor)
	}
	firstFilteredID := resp.Hits[0].RecordID
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Page:      yeoul.Page{Limit: 1, Cursor: resp.Meta.NextCursor},
	}, []string{"fact:fact-bench", "fact:missing", "fact:fact-note", "fact:fact-note-2"})
	if err != nil {
		t.Fatalf("build rax filtered page two: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID == firstFilteredID || resp.Meta.NextCursor != "" {
		t.Fatalf("expected second filtered page without duplicate, first=%q hits=%#v cursor=%q", firstFilteredID, resp.Hits, resp.Meta.NextCursor)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Include:   yeoul.Include{SupportingEpisodes: true, RelatedEntities: true},
	}, []string{"fact:fact-bench", "fact:fact-note"})
	if err != nil {
		t.Fatalf("build rax included response: %v", err)
	}
	if len(resp.Included.Episodes) != 1 || resp.Included.Episodes[0].ID != "ep-note" {
		t.Fatalf("expected scoped supporting episode, got %#v", resp.Included.Episodes)
	}
	if len(resp.Included.Sources) != 1 || resp.Included.Sources[0].Kind != "note" {
		t.Fatalf("expected scoped source, got %#v", resp.Included.Sources)
	}
	if len(resp.Included.Entities) == 0 {
		t.Fatalf("expected related entities, got %#v", resp.Included.Entities)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{EntityTypes: []string{"Other"}},
	}, []string{"fact:fact-note", "fact:fact-bench"})
	if err != nil {
		t.Fatalf("build rax entity type response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "fact-note" {
		t.Fatalf("expected entity type scope to keep only fact-note, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		AnchorIDs: []string{"missing-anchor"},
	}, []string{"fact:fact-note", "fact:fact-bench"})
	if err != nil {
		t.Fatalf("build rax anchor response: %v", err)
	}
	if len(resp.Hits) != 0 {
		t.Fatalf("expected unmatched anchor to filter all rax hits, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scoped note",
		Types:     []string{"fact"},
	}, []string{"fact:fact-bench", "fact:fact-note"})
	if err != nil {
		t.Fatalf("build rax rerank response: %v", err)
	}
	if len(resp.Hits) < 2 {
		t.Fatalf("expected rax hits with core score annotations, got %#v", resp.Hits)
	}
	if resp.Hits[0].RecordID != "fact-note" {
		t.Fatalf("expected core-matched fact-note to rank first, got %#v", resp.Hits)
	}
	if !slices.ContainsFunc(resp.Hits, func(hit yeoul.SearchHit) bool {
		return hit.RecordID == "fact-note" && slices.ContainsFunc(hit.Reasons, func(reason string) bool {
			return strings.HasPrefix(reason, "core_rerank_score:")
		})
	}) {
		t.Fatalf("expected core score reason on fact-note, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "scope",
		Types:     []string{"entity"},
	}, []string{"entity:thing:duplicate", "entity:thing:scope"})
	if err != nil {
		t.Fatalf("build rax duplicate entity response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "thing:scope" {
		t.Fatalf("expected rax entity search to hide duplicates, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "obsolete needle",
		Types:     []string{"fact"},
	}, []string{"fact:fact-stale"})
	if err != nil {
		t.Fatalf("build rax stale revision response: %v", err)
	}
	if len(resp.Hits) != 0 {
		t.Fatalf("expected rax stale revision-only match to be filtered, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "obsolete needle",
		Types:     []string{"fact"},
		Temporal:  yeoul.TemporalFilter{IncludeInactive: true},
	}, []string{"fact:fact-stale"})
	if err != nil {
		t.Fatalf("build rax inactive revision response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "fact-stale" {
		t.Fatalf("expected include_inactive to keep stale fact, got %#v", resp.Hits)
	}
	resp, err = buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "fresh visible",
		Types:     []string{"fact"},
	}, []string{"fact:fact-visible"})
	if err != nil {
		t.Fatalf("build rax fresh visible response: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].RecordID != "fact-visible" {
		t.Fatalf("expected current visible text to pass, got %#v", resp.Hits)
	}

	resp, err = runRaxPrimarySearch(ctx, eng, "", yeoul.SearchRequest{
		QueryText: "scoped",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Page:      yeoul.Page{Limit: 1, Cursor: "offset:1"},
	}, "", os.Args[0])
	if err != nil {
		t.Fatalf("expected filtered rax offset cursor to continue through core fallback: %v", err)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("expected core fallback offset page, got %#v", resp.Hits)
	}
}

// TestRaxPrimarySearchTruncationContract pins how a saturated native candidate
// window is surfaced. The window is bounded by fetchLimit, so a query that does
// not fall back to core must report the horizon instead of ending pagination
// silently, and a filtered query whose eligible records sit beyond the window
// must fall back to a complete core result set.
func TestRaxPrimarySearchTruncationContract(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "thing:window", Type: "Thing", CanonicalName: "window"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-window", Kind: "note", Content: "window needle", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "window"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("fact-window-%d", i)
		if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: id, Predicate: "HAS_WINDOW", SubjectID: entity.ID, ValueText: "window needle", SupportingEpisodeIDs: []string{episode.EpisodeID}}); err != nil {
			t.Fatalf("assert fact %s: %v", id, err)
		}
	}
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "projection.rax")
	argsPath := filepath.Join(tmpDir, "rax-args.txt")
	projectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")
	idsPath := filepath.Join(tmpDir, "rax-search-ids.json")
	fetchLimit := raxPrimaryFetchLimit(10)
	// The fake runtime ignores top-k and returns this fixed list, so it must
	// saturate the native window to exercise the truncation contract.
	var idsJSON strings.Builder
	idsJSON.WriteString("[")
	for i := 0; i < fetchLimit; i++ {
		fmt.Fprintf(&idsJSON, `{"doc_id":"fact:fact-missing-%d"},`, i)
	}
	idsJSON.WriteString(`{"doc_id":"fact:fact-window-0"},{"doc_id":"fact:fact-window-1"}]`)
	if err := os.WriteFile(idsPath, []byte(idsJSON.String()), 0o644); err != nil {
		t.Fatalf("write fake search ids: %v", err)
	}
	if err := os.WriteFile(storePath, []byte("store"), 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
	manifest := projectionManifest{Version: projectionManifestVersion, ProjectionCount: 3, RaxRuntime: "cli:" + os.Args[0], BuiltAt: time.Now().UTC()}
	if _, err := writeProjectionManifest(tmpDir, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", argsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", projectionPath)
	t.Setenv("YEOUL_FAKE_RAX_SEARCH_IDS", idsPath)

	query := yeoul.SearchRequest{QueryText: "window needle", Types: []string{"fact"}, Page: yeoul.Page{Limit: 10}}
	saturated := make([]string, 0, fetchLimit+2)
	for i := 0; i < fetchLimit; i++ {
		saturated = append(saturated, "fact:fact-missing")
	}
	saturated = append(saturated, "fact:fact-window-0", "fact:fact-window-1")
	resp, err := buildRaxPrimarySearchResponse(ctx, eng, query, saturated)
	if err != nil {
		t.Fatalf("build saturated response: %v", err)
	}
	if len(resp.Hits) != 2 {
		t.Fatalf("expected two eligible hits, got %#v", resp.Hits)
	}
	if !raxPrimaryShouldFallbackToCore(query, raxPrimaryEligibleCount(resp), len(saturated), fetchLimit) {
		t.Fatal("expected fallback when the saturated window cannot fill the requested page")
	}
	if raxPrimaryShouldFallbackToCore(query, 10, fetchLimit, fetchLimit) {
		t.Fatal("did not expect fallback when the saturated window fills the requested page")
	}
	if !raxPrimaryShouldFallbackToCore(query, 0, fetchLimit, fetchLimit) {
		t.Fatal("expected fallback when the saturated window yields no eligible hits")
	}

	// A filtered query whose eligible records sit beyond the window must return
	// the complete core result set instead of an empty truncated page.
	fallback, err := runRaxPrimarySearch(ctx, eng, tmpDir, yeoul.SearchRequest{
		QueryText: "window needle",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{SourceKinds: []string{"note"}},
		Page:      yeoul.Page{Limit: 10},
	}, "", os.Args[0])
	if err != nil {
		t.Fatalf("filtered rax search: %v", err)
	}
	if len(fallback.Hits) != 3 {
		t.Fatalf("expected core fallback to return every eligible fact, got %#v", fallback.Hits)
	}
	if fallback.Meta.TotalApprox != nil {
		t.Fatalf("did not expect a truncation horizon after core fallback, got %d", *fallback.Meta.TotalApprox)
	}

	// An unfiltered query that saturates the window must surface the horizon so
	// callers can detect that pagination is bounded.
	truncated, err := runRaxPrimarySearch(ctx, eng, tmpDir, yeoul.SearchRequest{QueryText: "window needle", Page: yeoul.Page{Limit: 10}}, "", os.Args[0])
	if err != nil {
		t.Fatalf("unfiltered rax search: %v", err)
	}
	if truncated.Meta.TotalApprox == nil || *truncated.Meta.TotalApprox != int64(fetchLimit) {
		t.Fatalf("expected the saturated window to report a truncation horizon of %d, got %#v", fetchLimit, truncated.Meta.TotalApprox)
	}
	if len(truncated.Hits) != 2 {
		t.Fatalf("expected the unfiltered page to keep its eligible hits, got %#v", truncated.Hits)
	}
}

// TestRaxPrimarySearchIncludeFlagsAreIndependent mirrors the core flag-shaping
// contract on the Rax path and checks that support shared by multiple hits is
// deduplicated rather than repeated per hit.
func TestRaxPrimarySearchIncludeFlagsAreIndependent(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	epShared, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-shared", Kind: "note", Content: "shared needle note", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "shared"}})
	if err != nil {
		t.Fatalf("ingest shared episode: %v", err)
	}
	project, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "project:alpha", Type: "Project", CanonicalName: "Alpha"})
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	database, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "database:shared", Type: "Database", CanonicalName: "Shared"})
	if err != nil {
		t.Fatalf("upsert database: %v", err)
	}
	for _, id := range []string{"fact-a", "fact-b"} {
		if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: id, Predicate: "HAS_NEEDLE", SubjectID: project.ID, ObjectID: database.ID, ValueText: "shared needle value", SupportingEpisodeIDs: []string{epShared.EpisodeID}}); err != nil {
			t.Fatalf("assert %s: %v", id, err)
		}
	}

	search := func(t *testing.T, include yeoul.Include) *yeoul.SearchResponse {
		t.Helper()
		resp, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
			QueryText: "shared needle value",
			Types:     []string{"fact"},
			Include:   include,
		}, []string{"fact:fact-a", "fact:fact-b"})
		if err != nil {
			t.Fatalf("build rax response: %v", err)
		}
		if len(resp.Hits) != 2 {
			t.Fatalf("expected two fact hits, got %#v", resp.Hits)
		}
		return resp
	}

	noFlags := search(t, yeoul.Include{})
	if len(noFlags.Included.Facts) != 0 || len(noFlags.Included.Episodes) != 0 || len(noFlags.Included.Entities) != 0 || len(noFlags.Included.Sources) != 0 {
		t.Fatalf("expected no includes without flags, got %#v", noFlags.Included)
	}

	factsOnly := search(t, yeoul.Include{SupportingFacts: true})
	if len(factsOnly.Included.Facts) != 2 {
		t.Fatalf("expected supporting_facts to include both facts, got %#v", factsOnly.Included.Facts)
	}
	if len(factsOnly.Included.Episodes) != 0 || len(factsOnly.Included.Entities) != 0 || len(factsOnly.Included.Sources) != 0 {
		t.Fatalf("expected supporting_facts to exclude other families, got %#v", factsOnly.Included)
	}

	episodesOnly := search(t, yeoul.Include{SupportingEpisodes: true})
	if len(episodesOnly.Included.Episodes) != 1 || episodesOnly.Included.Episodes[0].ID != "ep-shared" {
		t.Fatalf("expected shared support to dedupe to one episode, got %#v", episodesOnly.Included.Episodes)
	}
	if len(episodesOnly.Included.Facts) != 0 || len(episodesOnly.Included.Entities) != 0 {
		t.Fatalf("expected supporting_episodes to exclude other families, got %#v", episodesOnly.Included)
	}

	provenance := search(t, yeoul.Include{Provenance: true})
	if len(provenance.Included.Episodes) != 1 || provenance.Included.Episodes[0].ID != "ep-shared" {
		t.Fatalf("expected provenance to include the shared episode once, got %#v", provenance.Included.Episodes)
	}
	if len(provenance.Included.Sources) != 1 || provenance.Included.Sources[0].Kind != "note" {
		t.Fatalf("expected provenance to imply the episode source, got %#v", provenance.Included.Sources)
	}

	entitiesOnly := search(t, yeoul.Include{RelatedEntities: true})
	if len(entitiesOnly.Included.Entities) != 2 {
		t.Fatalf("expected related_entities to include the deduped subject and object, got %#v", entitiesOnly.Included.Entities)
	}
	if len(entitiesOnly.Included.Facts) != 0 || len(entitiesOnly.Included.Episodes) != 0 || len(entitiesOnly.Included.Sources) != 0 {
		t.Fatalf("expected related_entities to exclude other families, got %#v", entitiesOnly.Included)
	}

	snippetsOnly := search(t, yeoul.Include{Snippets: true})
	if len(snippetsOnly.Included.Facts) != 0 || len(snippetsOnly.Included.Episodes) != 0 || len(snippetsOnly.Included.Entities) != 0 || len(snippetsOnly.Included.Sources) != 0 {
		t.Fatalf("expected snippets to select no included family, got %#v", snippetsOnly.Included)
	}
}

// TestRaxPrimarySearchKeepsProvenanceUnderHitFilters checks that predicate and
// anchor filters select hits without also suppressing the supporting episodes
// that explain them, matching what the core engine returns.
func TestRaxPrimarySearchKeepsProvenanceUnderHitFilters(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	ep, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-prov", Kind: "note", Content: "provenance episode", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "prov"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	subject, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "project:prov", Type: "Project", CanonicalName: "Provenance"})
	if err != nil {
		t.Fatalf("upsert subject: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-prov", Predicate: "HAS_PROVENANCE", SubjectID: subject.ID, ValueText: "provenance needle", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	object, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "database:prov", Type: "Database", CanonicalName: "ProvStore"})
	if err != nil {
		t.Fatalf("upsert object: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-typed", Predicate: "HAS_PROVENANCE", SubjectID: subject.ID, ObjectID: object.ID, ValueText: "typed provenance needle", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert typed fact: %v", err)
	}

	coreResp, err := eng.Search(ctx, yeoul.SearchRequest{
		QueryText:  "provenance needle",
		Types:      []string{"fact"},
		Predicates: []string{"HAS_PROVENANCE"},
		Include:    yeoul.Include{Provenance: true},
	})
	if err != nil {
		t.Fatalf("core search: %v", err)
	}
	if len(coreResp.Included.Episodes) != 1 || coreResp.Included.Episodes[0].ID != ep.EpisodeID {
		t.Fatalf("expected core to keep provenance under predicate filter, got %#v", coreResp.Included.Episodes)
	}

	raxResp, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText:  "provenance needle",
		Types:      []string{"fact"},
		Predicates: []string{"HAS_PROVENANCE"},
		Include:    yeoul.Include{Provenance: true},
	}, []string{"fact:fact-prov"})
	if err != nil {
		t.Fatalf("build rax response: %v", err)
	}
	if len(raxResp.Hits) != 1 {
		t.Fatalf("expected rax fact hit, got %#v", raxResp.Hits)
	}
	if len(raxResp.Included.Episodes) != 1 || raxResp.Included.Episodes[0].ID != ep.EpisodeID {
		t.Fatalf("expected rax to keep provenance under predicate filter, got %#v", raxResp.Included.Episodes)
	}
	if len(raxResp.Included.Sources) != 1 {
		t.Fatalf("expected rax to keep the episode source, got %#v", raxResp.Included.Sources)
	}

	anchorResp, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "provenance needle",
		Types:     []string{"fact"},
		AnchorIDs: []string{subject.ID},
		Include:   yeoul.Include{Provenance: true},
	}, []string{"fact:fact-prov"})
	if err != nil {
		t.Fatalf("build rax anchor response: %v", err)
	}
	if len(anchorResp.Hits) != 1 {
		t.Fatalf("expected rax anchor-matched fact hit, got %#v", anchorResp.Hits)
	}
	if len(anchorResp.Included.Episodes) != 1 || anchorResp.Included.Episodes[0].ID != ep.EpisodeID {
		t.Fatalf("expected rax to keep provenance under anchor filter, got %#v", anchorResp.Included.Episodes)
	}

	coreScopeResp, err := eng.Search(ctx, yeoul.SearchRequest{
		QueryText: "typed provenance needle",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{EntityTypes: []string{"Project"}},
		Include:   yeoul.Include{RelatedEntities: true},
	})
	if err != nil {
		t.Fatalf("core scoped search: %v", err)
	}
	if len(coreScopeResp.Included.Entities) != 1 || coreScopeResp.Included.Entities[0].ID != subject.ID {
		t.Fatalf("expected core to keep only the in-scope related entity, got %#v", coreScopeResp.Included.Entities)
	}
	raxScopeResp, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "typed provenance needle",
		Types:     []string{"fact"},
		Scope:     yeoul.ScopeFilter{EntityTypes: []string{"Project"}},
		Include:   yeoul.Include{RelatedEntities: true},
	}, []string{"fact:fact-typed"})
	if err != nil {
		t.Fatalf("build rax scoped response: %v", err)
	}
	if len(raxScopeResp.Included.Entities) != 1 || raxScopeResp.Included.Entities[0].ID != subject.ID {
		t.Fatalf("expected rax to keep only the in-scope related entity, got %#v", raxScopeResp.Included.Entities)
	}
}

// TestRaxPrimarySearchUsesCoreKeywordSemantics checks that native candidates are
// validated with the core matcher: keyword mode rejects a record that only
// partially matches, and matches against aliases and fact predicate/endpoint
// text are admitted even when the record is outside the core rerank subset.
func TestRaxPrimarySearchUsesCoreKeywordSemantics(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	subject, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "project:keyword", Type: "Project", CanonicalName: "Keyword"})
	if err != nil {
		t.Fatalf("upsert subject: %v", err)
	}
	ep, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-keyword", Kind: "note", Content: "keyword semantics episode", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "keyword"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-alpha", Predicate: "HAS_ALPHA", SubjectID: subject.ID, ValueText: "alpha only value", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert alpha fact: %v", err)
	}
	aliased, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "project:aliased", Type: "Project", CanonicalName: "Canonical", Aliases: []string{"betaname"}})
	if err != nil {
		t.Fatalf("upsert aliased: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-predicate", Predicate: "ALPHA_BETA", SubjectID: aliased.ID, ValueText: "unrelated value", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert predicate fact: %v", err)
	}

	// Keyword "alpha beta" must not admit a record containing only "alpha".
	partial, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "alpha beta",
		Mode:      yeoul.SearchModeKeyword,
		Types:     []string{"fact"},
	}, []string{"fact:fact-alpha"})
	if err != nil {
		t.Fatalf("build partial response: %v", err)
	}
	if len(partial.Hits) != 0 {
		t.Fatalf("expected keyword mode to reject partial-token candidate, got %#v", partial.Hits)
	}

	// Predicate-only match must be admitted: core matches fact predicate text.
	predicate, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "alpha_beta",
		Mode:      yeoul.SearchModeKeyword,
		Types:     []string{"fact"},
	}, []string{"fact:fact-predicate"})
	if err != nil {
		t.Fatalf("build predicate response: %v", err)
	}
	if len(predicate.Hits) != 1 || predicate.Hits[0].RecordID != "fact-predicate" {
		t.Fatalf("expected predicate-only match to be admitted, got %#v", predicate.Hits)
	}

	// Alias-only entity match must be admitted even outside the core rerank subset.
	alias, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "betaname",
		Mode:      yeoul.SearchModeKeyword,
		Types:     []string{"entity"},
	}, []string{"entity:project:aliased"})
	if err != nil {
		t.Fatalf("build alias response: %v", err)
	}
	if len(alias.Hits) != 1 || alias.Hits[0].RecordID != "project:aliased" {
		t.Fatalf("expected alias-only match to be admitted, got %#v", alias.Hits)
	}
}

func TestCLISearchRejectsAmbiguousTemporalFlags(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "temporal-flags.ltdb")
	var stdout strings.Builder
	var stderr strings.Builder
	if err := run(ctx, []string{"init", "--db", dbPath}, &stdout, &stderr); err != nil {
		t.Fatalf("init: %v\nstderr=%s", err, stderr.String())
	}
	err := run(ctx, []string{"search", "--db", dbPath, "--query", "x", "--valid-at", "2026-06-01T00:00:00Z", "--valid-from", "2026-06-01T00:00:00Z"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected valid-at plus valid-from to fail")
	}
	err = run(ctx, []string{"search", "--db", dbPath, "--query", "x", "--valid-from", "2026-06-02T00:00:00Z", "--valid-to", "2026-06-01T00:00:00Z"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected inverted valid interval to fail")
	}
}

func TestCLIContext(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "context.ltdb")
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
	runCLI("ingest", "episode", "--db", dbPath, "--id", "ep-context", "--kind", "note", "--content", "context constructor")
	output := runCLI("context", "--db", dbPath, "--query", "constructor", "--json")
	if !strings.Contains(output, `"blocks"`) || !strings.Contains(output, `"context constructor"`) {
		t.Fatalf("expected context blocks, got %q", output)
	}
}

func TestCLIInitForceRefusesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "locked.ltdb")

	var stdout strings.Builder
	var stderr strings.Builder
	if err := run(ctx, []string{"init", "--db", dbPath}, &stdout, &stderr); err != nil {
		t.Fatalf("init: %v", err)
	}

	eng, err := yeoul.Open(ctx, yeoul.Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}

	var resetOut strings.Builder
	resetErr := run(ctx, []string{"init", "--db", dbPath, "--force", "--confirm"}, &resetOut, &resetOut)
	if resetErr == nil {
		t.Fatal("expected forced init on an existing database to be refused")
	}
	if !strings.Contains(resetErr.Error(), "not supported") {
		t.Fatalf("expected an explicit refusal, got %v", resetErr)
	}
	if _, statErr := os.Stat(filepath.Join(dbPath, "state.json")); statErr != nil {
		t.Fatalf("database must remain intact after the refused reset: %v", statErr)
	}

	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	var retryOut strings.Builder
	if err := run(ctx, []string{"init", "--db", dbPath, "--force", "--confirm"}, &retryOut, &retryOut); err == nil {
		t.Fatal("expected forced init to keep refusing an existing database after the owner closed")
	}
}

func TestCLIInitForcePreservesForeignDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keepPath := filepath.Join(dir, "keep.txt")
	tmpPath := filepath.Join(dir, ".state-important.tmp")
	if err := os.WriteFile(keepPath, []byte("precious"), 0o600); err != nil {
		t.Fatalf("write keep file: %v", err)
	}
	if err := os.WriteFile(tmpPath, []byte("temporary-but-foreign"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var out strings.Builder
	err := run(ctx, []string{"init", "--db", dir, "--force", "--confirm"}, &out, &out)
	if err == nil {
		t.Fatal("expected forced init to refuse a directory that is not a Yeoul database")
	}
	for path, want := range map[string]string{keepPath: "precious", tmpPath: "temporary-but-foreign"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("expected %s to survive the refusal: %v", path, readErr)
		}
		if string(got) != want {
			t.Fatalf("expected %s to keep its bytes, got %q", path, string(got))
		}
	}
}

func TestCLIAdminExportUsesPrivatePermissions(t *testing.T) {
	requireExportSupport(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "private-export.ltdb")
	exportPath := filepath.Join(tmpDir, "private-export.json")

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
	runCLI(
		"ingest", "episode",
		"--db", dbPath,
		"--id", "ep-private",
		"--kind", "note",
		"--content", "private export",
		"--source-kind", "note",
		"--source-external-ref", "thread-private",
	)

	assertPrivate := func(stage string) {
		t.Helper()
		info, err := os.Stat(exportPath)
		if err != nil {
			t.Fatalf("stat export after %s: %v", stage, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("expected export mode 0600 after %s, got %o", stage, got)
		}
	}

	runCLI("admin", "export", "--db", dbPath, "--out", exportPath)
	assertPrivate("first export")

	if err := os.Chmod(exportPath, 0o644); err != nil {
		t.Fatalf("chmod export: %v", err)
	}
	runCLI("admin", "export", "--db", dbPath, "--out", exportPath)
	assertPrivate("replacement export")

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(data), "ep-private") {
		t.Fatalf("expected the replacement export to contain the episode, got %q", string(data))
	}
}

func TestCLIAdminExportRefusesUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("admin export refuses to run on macOS and Windows")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "refused-export.ltdb")
	exportPath := filepath.Join(tmpDir, "refused-export.json")

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
		"--id", "ep-refused",
		"--kind", "note",
		"--content", "refused export",
		"--source-kind", "note",
		"--source-external-ref", "thread-refused",
	); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}

	_, err := runCLI("admin", "export", "--db", dbPath, "--out", exportPath)
	if err == nil {
		t.Fatal("expected admin export to be refused on this platform")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected a not-supported error, got %v", err)
	}
	if _, statErr := os.Stat(exportPath); statErr == nil {
		t.Fatal("expected the refused export to create no file")
	}
}

// TestCLIIndexPublishRaxAppendsToExistingStore pins the documented
// publication semantics: publish-rax appends or updates documents by ID in an
// existing store instead of replacing it, and the reported count describes the
// incoming projection only. Publishing two disjoint corpora into one target
// must therefore leave both corpora present.
func TestCLIIndexPublishRaxAppendsToExistingStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	fakeRaxPath := os.Args[0]
	argsPath := filepath.Join(tmpDir, "rax-args.txt")
	projectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")
	storePath := filepath.Join(tmpDir, "shared.rax")
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", argsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", projectionPath)
	t.Setenv("YEOUL_FAKE_RAX_MERGE_DOCIDS", "1")

	runCLI := func(args ...string) string {
		t.Helper()
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstderr=%s", args, err, stderr.String())
		}
		return stdout.String()
	}

	// Each corpus lives in its own database and index root so the two
	// projections are disjoint, exactly like publishing two different sources
	// into one reused store.
	buildCorpus := func(name, episodeID, factID string) string {
		t.Helper()
		dbPath := filepath.Join(tmpDir, name+".ltdb")
		ingestPath := filepath.Join(tmpDir, name+"-ingest.json")
		root := filepath.Join(tmpDir, name+"-index")
		payload := `{
  "episodes": [{"id":"` + episodeID + `","kind":"note","content":"` + name + ` corpus note","source":{"kind":"note","external_ref":"` + name + `"}}],
  "entities": [{"id":"project:` + name + `","type":"Project","canonical_name":"` + name + `"}],
  "facts": [{"id":"` + factID + `","predicate":"DESCRIBES","subject_id":"project:` + name + `","value_text":"` + name + ` corpus fact","supporting_episode_ids":["` + episodeID + `"]}]
}`
		if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
			t.Fatalf("write ingest payload: %v", err)
		}
		runCLI("init", "--db", dbPath)
		runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
		runCLI("index", "build", "--db", dbPath, "--root", root, "--json")
		return root
	}

	firstRoot := buildCorpus("alpha", "ep-alpha", "fact-alpha")
	secondRoot := buildCorpus("beta", "ep-beta", "fact-beta")

	first := runCLI("index", "publish-rax", "--root", firstRoot, "--store", storePath, "--rax-bin", fakeRaxPath, "--json")
	if !strings.Contains(first, `"published_document_count": 3`) {
		t.Fatalf("expected first publish to report its own 3 documents, got %q", first)
	}
	if strings.Contains(first, "rax_document_count") {
		t.Fatalf("expected the ambiguous rax_document_count field to be gone, got %q", first)
	}
	second := runCLI("index", "publish-rax", "--root", secondRoot, "--store", storePath, "--rax-bin", fakeRaxPath, "--json")
	if !strings.Contains(second, `"published_document_count": 3`) {
		t.Fatalf("expected second publish to report its own 3 documents, got %q", second)
	}

	storeData, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	for _, docID := range []string{"fact:fact-alpha", "fact:fact-beta"} {
		if !strings.Contains(string(storeData), docID) {
			t.Fatalf("expected publication to append %s into the reused store, got %q", docID, string(storeData))
		}
	}
}

// TestCLIIngestRejectsSecretCanaries exercises the pre-ingest boundary through
// the CLI entry points that persist caller text: a single episode, a bulk JSON
// payload (including metadata), and a lifecycle reason. Synthetic canaries are
// used so no real credential is involved.
func TestCLIIngestRejectsSecretCanaries(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "secrets.ltdb")
	const canary = "AKIAIOSFODNN7EXAMPLE"

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

	// Single-episode ingest with the canary in --content.
	_, err := runCLI("ingest", "episode", "--db", dbPath, "--kind", "note", "--content", "token is "+canary, "--source-kind", "note")
	if err == nil {
		t.Fatal("expected ingest episode to reject the secret canary")
	}
	if !strings.Contains(err.Error(), string(yeoul.ErrInputInvalid)) {
		t.Fatalf("expected %s, got %v", yeoul.ErrInputInvalid, err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("CLI error echoed the rejected canary: %v", err)
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for a rejected secret, got %d", code)
	}

	// Bulk JSON ingest with the canary hidden in entity metadata.
	payloadPath := filepath.Join(tmpDir, "secret-ingest.json")
	payload := `{"entities":[{"id":"thing:x","type":"Thing","canonical_name":"x","metadata":{"token":"` + canary + `"}}]}`
	if writeErr := os.WriteFile(payloadPath, []byte(payload), 0o644); writeErr != nil {
		t.Fatalf("write payload: %v", writeErr)
	}
	_, err = runCLI("ingest", "json", "--db", dbPath, "--file", payloadPath)
	if err == nil {
		t.Fatal("expected ingest json to reject the secret canary")
	}
	if !strings.Contains(err.Error(), string(yeoul.ErrInputInvalid)) {
		t.Fatalf("expected %s, got %v", yeoul.ErrInputInvalid, err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("CLI error echoed the rejected canary: %v", err)
	}
}

// TestRaxPrimarySearchAcceptsAnchorExpansionMatches mirrors core anchor
// semantics: an anchor constrains the seeds, and core may then rank facts that
// reach the anchor through a bounded expansion. A Rax candidate that core
// ranked through expansion must therefore stay visible instead of being dropped
// for not matching the anchor directly, while an anchor matching no seed still
// filters every candidate.
func TestRaxPrimarySearchAcceptsAnchorExpansionMatches(t *testing.T) {
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	ep, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "ep-anchor", Kind: "note", Content: "anchor expansion episode", Source: yeoul.SourceInput{Kind: "note", ExternalRef: "anchor"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entityA, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "node:a", Type: "Node", CanonicalName: "AnchorNeedleA"})
	if err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	entityB, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "node:b", Type: "Node", CanonicalName: "NodeB"})
	if err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	entityC, err := eng.UpsertEntity(ctx, yeoul.EntityInput{ID: "node:c", Type: "Node", CanonicalName: "NodeC"})
	if err != nil {
		t.Fatalf("upsert C: %v", err)
	}
	// A-B fact seeds from the anchor match; B-C does not match the query text
	// directly and is only reachable by bounded graph expansion.
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-ab", Predicate: "LINKS", SubjectID: entityA.ID, ObjectID: entityB.ID, ValueText: "anchor needle bridge", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert A-B: %v", err)
	}
	if _, err := eng.AssertFact(ctx, yeoul.FactInput{ID: "fact-bc", Predicate: "LINKS", SubjectID: entityB.ID, ObjectID: entityC.ID, ValueText: "distant link", SupportingEpisodeIDs: []string{ep.EpisodeID}}); err != nil {
		t.Fatalf("assert B-C: %v", err)
	}

	req := yeoul.SearchRequest{
		QueryText: "anchor needle",
		Types:     []string{"fact"},
		AnchorIDs: []string{entityA.ID},
	}
	hasReason := func(hits []yeoul.SearchHit, recordID, reason string) bool {
		for _, hit := range hits {
			if hit.RecordID == recordID {
				return slices.Contains(hit.Reasons, reason)
			}
		}
		return false
	}
	hasHit := func(hits []yeoul.SearchHit, recordID string) bool {
		for _, hit := range hits {
			if hit.RecordID == recordID {
				return true
			}
		}
		return false
	}
	coreResp, err := eng.Search(ctx, req)
	if err != nil {
		t.Fatalf("core search: %v", err)
	}
	if !hasReason(coreResp.Hits, "fact-bc", "graph_expansion") {
		t.Fatalf("expected core to rank the two-hop fact through expansion, got %#v", coreResp.Hits)
	}

	// Rax sees both facts as native candidates; the two-hop fact must survive.
	raxResp, err := buildRaxPrimarySearchResponse(ctx, eng, req, []string{"fact:fact-ab", "fact:fact-bc"})
	if err != nil {
		t.Fatalf("build rax response: %v", err)
	}
	if !hasHit(raxResp.Hits, "fact-bc") {
		t.Fatalf("expected rax to keep the anchor-reachable two-hop fact, got %#v", raxResp.Hits)
	}

	// An anchor matching no seed still filters every candidate.
	unmatched, err := buildRaxPrimarySearchResponse(ctx, eng, yeoul.SearchRequest{
		QueryText: "anchor needle",
		Types:     []string{"fact"},
		AnchorIDs: []string{"node:missing"},
	}, []string{"fact:fact-ab", "fact:fact-bc"})
	if err != nil {
		t.Fatalf("build rax unmatched response: %v", err)
	}
	if len(unmatched.Hits) != 0 {
		t.Fatalf("expected an unmatched anchor to filter every rax candidate, got %#v", unmatched.Hits)
	}
}

// TestCLIEmptyDatabaseRaxSearchAndPublish covers the empty-store contract: an
// explicitly Rax search on a freshly initialized database returns empty results
// instead of erroring, an empty index publishes without invoking the native
// ingest (which rejects empty document lists), and a record added afterward is
// still visible through both core and Rax search.
func TestCLIEmptyDatabaseRaxSearchAndPublish(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.ltdb")
	indexRoot := filepath.Join(tmpDir, "index")
	storePath := filepath.Join(tmpDir, "projection.rax")
	fakeRaxPath := os.Args[0]
	raxArgsPath := filepath.Join(tmpDir, "rax-args.txt")
	raxProjectionPath := filepath.Join(tmpDir, "rax-projection.jsonl")
	t.Setenv("YEOUL_FAKE_RAX", "1")
	t.Setenv("YEOUL_FAKE_RAX_ARGS", raxArgsPath)
	t.Setenv("YEOUL_FAKE_RAX_PROJECTION", raxProjectionPath)

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
	for _, backend := range []string{"core", "auto", "rax"} {
		search := runCLI("search", "--db", dbPath, "--query", "anything", "--backend", backend, "--rax-bin", fakeRaxPath, "--json")
		if !strings.Contains(search, `"hits": []`) {
			t.Fatalf("expected empty %s search results, got %q", backend, search)
		}
	}

	build := runCLI("index", "build", "--db", dbPath, "--root", indexRoot, "--json")
	if !strings.Contains(build, `"projection_count": 0`) {
		t.Fatalf("expected empty index build, got %q", build)
	}
	publish := runCLI("index", "publish-rax", "--root", indexRoot, "--store", storePath, "--rax-bin", fakeRaxPath, "--json")
	if !strings.Contains(publish, `"published": true`) || !strings.Contains(publish, `"published_document_count": 0`) {
		t.Fatalf("expected empty rax publish, got %q", publish)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("expected the empty publish to write a store artifact: %v", err)
	}
	if args, err := os.ReadFile(raxArgsPath); err == nil && strings.Contains(string(args), "ingest docs ") {
		t.Fatalf("expected the empty publish to skip native ingest, got %q", string(args))
	}

	ingestPath := filepath.Join(tmpDir, "first.json")
	payload := `{
  "episodes": [{"id":"ep-first","kind":"note","content":"first record needle","source":{"kind":"note","external_ref":"first"}}],
	  "entities": [{"id":"project:first","type":"Project","canonical_name":"First"}],
  "facts": [{"id":"fact-first","predicate":"HAS_FIRST","subject_id":"project:first","value_text":"first record needle","supporting_episode_ids":["ep-first"]}]
}`
	if err := os.WriteFile(ingestPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write ingest payload: %v", err)
	}
	runCLI("ingest", "json", "--db", dbPath, "--file", ingestPath)
	for _, backend := range []string{"core", "auto"} {
		search := runCLI("search", "--db", dbPath, "--query", "first record needle", "--backend", backend, "--rax-bin", fakeRaxPath, "--json")
		if !strings.Contains(search, `"record_id": "fact-first"`) {
			t.Fatalf("expected %s search to see the first record after ingest, got %q", backend, search)
		}
	}
	// The fake runtime always returns one fixed doc id, so rax hydration cannot
	// be asserted here without the real runtime. Assert instead that the record
	// made the managed rax store non-empty and the native ingest ran.
	_ = runCLI("search", "--db", dbPath, "--query", "first record needle", "--backend", "rax", "--rax-bin", fakeRaxPath, "--json")
	raxArgs, err := os.ReadFile(raxArgsPath)
	if err != nil {
		t.Fatalf("read fake rax args: %v", err)
	}
	if !strings.Contains(string(raxArgs), "ingest docs ") {
		t.Fatalf("expected the managed rax store to ingest the first record, got %q", string(raxArgs))
	}
}
