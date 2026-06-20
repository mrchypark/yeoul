package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
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
		_, _ = os.Stdout.WriteString(`[{"doc_id":"fact:fact-index"}]`)
		return 0
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

func TestCLIInitIngestEpisodeAndSearch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.lbug")

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
	dbPath := filepath.Join(tmpDir, "yeoul.lbug")
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
	dbPath := filepath.Join(tmpDir, "yeoul.lbug")
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
	dbPath := filepath.Join(tmpDir, "yeoul.lbug")
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

	recipes := runCLI("policy", "list-recipes", "--path", packPath)
	if !strings.Contains(recipes, "recent_context") || !strings.Contains(recipes, "project_memory") {
		t.Fatalf("expected recipe list output, got %q", recipes)
	}
}

func TestCLIIndexBuildStatusAndVerify(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.lbug")
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
      "content":"Yeoul keeps Ladybug as canonical truth and uses rax as derived retrieval.",
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
	if !strings.Contains(publish, `"published": true`) || !strings.Contains(publish, `"rax_runtime": "cli:`) || !strings.Contains(publish, `"rax_document_count": 4`) {
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

func TestRaxProjectionChunkIDsMapBackToRecords(t *testing.T) {
	kind, id, ok := raxRecordKindID("episode:project:thread" + raxChunkMarker + "2")
	if !ok || kind != "episode" || id != "project:thread" {
		t.Fatalf("expected chunk doc id to map to original record, got kind=%q id=%q ok=%v", kind, id, ok)
	}
}

func TestRaxProjectionIncludesRevisionText(t *testing.T) {
	projections, manifest := buildProjectionArtifacts("test.lbug", &exportFile{
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
	dbPath := filepath.Join(tmpDir, "index-corrupt.lbug")
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
	dbPath := filepath.Join(tmpDir, "index-large.lbug")
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
	dbPath := filepath.Join(tmpDir, "index-pre-epoch.lbug")
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
	dbPath := filepath.Join(tmpDir, "index-unsafe-path.lbug")
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

func TestCLIAdminExportImport(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yeoul.lbug")
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
	updateEntityPath := filepath.Join(tmpDir, "update-entity-export.json")
	updateEntityPayload := `{
  "entities": [{"id":"thing:export","type":"Thing","canonical_name":"export renamed"}]
}`
	if err := os.WriteFile(updateEntityPath, []byte(updateEntityPayload), 0o644); err != nil {
		t.Fatalf("write update entity payload: %v", err)
	}
	runCLI("admin", "import", "--confirm", "--db", dbPath, "--in", updateEntityPath)
	runCLI("fact", "supersede", "--confirm", "--db", dbPath, "--id", "fact-export", "--predicate", "HAS_STATE", "--subject-id", "thing:export", "--value-text", "new export state", "--supporting-episodes", "ep-export", "--reason", "test")
	runCLI("admin", "export", "--db", dbPath, "--out", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(data), `"ep-export"`) {
		t.Fatalf("expected export payload to contain episode id, got %q", string(data))
	}
	if !strings.Contains(string(data), `"fact_revisions"`) || !strings.Contains(string(data), `"old export state"`) {
		t.Fatalf("expected export payload to contain fact revisions, got %q", string(data))
	}
	if !strings.Contains(string(data), `"entity_revisions"`) || !strings.Contains(string(data), `"export renamed"`) {
		t.Fatalf("expected export payload to contain entity revisions, got %q", string(data))
	}

	importedDB := filepath.Join(tmpDir, "imported.lbug")
	runCLI("init", "--db", importedDB)
	if err := runCLIError("admin", "import", "--confirm", "--db", importedDB, "--in", exportPath); err == nil {
		t.Fatal("expected admin import to reject revision restore payload")
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
	restoreDB := filepath.Join(tmpDir, "restore.lbug")
	runCLI("init", "--db", restoreDB)
	if err := runCLIError("admin", "import", "--confirm", "--db", restoreDB, "--in", restorePath); err == nil {
		t.Fatal("expected admin import to reject lifecycle-managed fact fields")
	}
}

func TestCLIPolicyDrivenSearchAndIngestDrop(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "policy.lbug")
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
	if err := os.WriteFile(filepath.Join(dropPack, "episode_rules.yaml"), []byte("version: 1\ndrop:\n  - name: drop_me\n    when:\n      contains_any: [\"ignore me\"]\n"), 0o644); err != nil {
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

func TestCLIBenchIngest(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bench.lbug")

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
	dbPath := filepath.Join(tmpDir, "aliases.lbug")
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

func TestCLITimelineProvenanceAndFactLookup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "query.lbug")
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
	dbPath := filepath.Join(tmpDir, "fact-upsert-subject.lbug")
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
	for _, want := range []string{
		`"predicate": "HAS_DECISION"`,
		`"subject_id": "decisiontopic:structured-memory-promotion"`,
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
		"--subject-id", "decisiontopic:structured-memory-promotion",
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
		`"subject_id": "project:yeoul"`,
		`"object_id": "database:ladybug"`,
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
	if !strings.Contains(stableKeyFact, `"subject_id": "feature:feature-stable-1"`) {
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
	if !strings.Contains(stableKeyFact, `"subject_id": "feature:feature-stable-1"`) {
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
	dbPath := filepath.Join(tmpDir, "fact-upsert-atomic.lbug")

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
	dbPath := filepath.Join(tmpDir, "prov-inactive.lbug")
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
}

func TestCLIEntityMergePreviewAndCompact(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "compact.lbug")
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

func TestCLIBenchQueryAndLifecycle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bench-plus.lbug")
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

	lifecycleDB := filepath.Join(tmpDir, "lifecycle.lbug")
	runCLI("init", "--db", lifecycleDB)
	lifecycleOutput := runCLI("bench", "lifecycle", "--db", lifecycleDB, "--iterations", "2", "--json")
	if !strings.Contains(lifecycleOutput, `"supersede_count": 2`) || !strings.Contains(lifecycleOutput, `"retraction_count": 2`) {
		t.Fatalf("expected lifecycle output, got %q", lifecycleOutput)
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
}

func TestCLISearchRejectsAmbiguousTemporalFlags(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "temporal-flags.lbug")
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
	dbPath := filepath.Join(tmpDir, "context.lbug")
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
