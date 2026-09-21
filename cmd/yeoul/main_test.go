package main

import (
	"context"
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
	os.Exit(m.Run())
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

	payload := `{
  "episodes": [
    {
      "id":"ep-index",
      "kind":"note",
      "content":"Yeoul keeps LatticeDB as canonical truth and runs retrieval through the core engine.",
      "source":{"kind":"note","external_ref":"thread-index"}
    }
  ],
  "entities": [
    {"id":"project:yeoul","type":"Project","canonical_name":"Yeoul"},
    {"id":"runtime:core","type":"Runtime","canonical_name":"core"}
  ],
  "facts": [
    {
      "id":"fact-index",
      "predicate":"USES_RETRIEVAL_RUNTIME",
      "subject_id":"project:yeoul",
      "object_id":"runtime:core",
      "value_text":"Yeoul uses the core engine as the retrieval runtime.",
      "supporting_episode_ids":["ep-index"]
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
	if !strings.Contains(string(projectionData), `"projection_id":"fact:fact-index"`) || !strings.Contains(string(projectionData), `"search_text":"USES_RETRIEVAL_RUNTIME project:yeoul runtime:core Yeoul uses the core engine as the retrieval runtime."`) {
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

	search := runCLI("search", "--db", dbPath, "--query", "retrieval runtime", "--json")
	if !strings.Contains(search, `"record_id": "fact-index"`) {
		t.Fatalf("expected search to surface the ingested fact, got %q", search)
	}
	bench := runCLI("bench", "query", "--db", dbPath, "--query", "retrieval runtime", "--iterations", "1", "--json")
	if !strings.Contains(bench, `"search"`) {
		t.Fatalf("expected bench query to exercise search, got %q", bench)
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

func TestProjectionIncludesRevisionText(t *testing.T) {
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
