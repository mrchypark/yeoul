package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		"--kind", "note",
		"--content", "Ladybug remains an internal storage concern.",
		"--source-kind", "note",
		"--source-external-ref", "thread-1",
	)

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

	importedDB := filepath.Join(tmpDir, "imported.lbug")
	runCLI("init", "--db", importedDB)
	runCLI("admin", "import", "--confirm", "--db", importedDB, "--in", exportPath)

	search := runCLI("search", "--db", importedDB, "--query", "export me")
	if !strings.Contains(search, "export me") {
		t.Fatalf("expected imported search result, got %q", search)
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
