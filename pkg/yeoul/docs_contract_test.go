package yeoul

// DOC-04 (#90) contract checks. The memory-model and query references under
// docs/ are accepted inputs, so the states, filters, and defaults they
// advertise must match the executable contract.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDocumentedFactStatusesMatchImplementation(t *testing.T) {
	const docPath = "docs/04-memory-model/temporal-semantics.md"
	body := readRepositoryDoc(t, docPath)

	for _, status := range []string{factStatusActive, factStatusSuperseded, factStatusRetracted} {
		if !validFactStatus(status) {
			t.Fatalf("documented status %q is rejected by validFactStatus", status)
		}
		if !strings.Contains(body, status) {
			t.Fatalf("%s does not document the implemented status %q", docPath, status)
		}
	}

	for _, unsupported := range []string{"contradicted", "uncertain", "archived", "expired_at"} {
		if validFactStatus(unsupported) {
			t.Fatalf("validFactStatus accepts undocumented status %q", unsupported)
		}
	}

	// The prose may mention future states, but it must mark them as not implemented.
	if !strings.Contains(body, "are not implemented") {
		t.Fatalf("%s must state which lifecycle states are not implemented", docPath)
	}
	if strings.Contains(body, "### expired_at") {
		t.Fatalf("%s still documents an expired_at field that the model lacks", docPath)
	}

	lifecycle := readRepositoryDoc(t, "docs/04-memory-model/fact-lifecycle.md")
	if !strings.Contains(lifecycle, "Implemented states:") || !strings.Contains(lifecycle, "future semantics") {
		t.Fatal("docs/04-memory-model/fact-lifecycle.md must separate implemented states from future semantics")
	}
}

func TestDocumentedQueryDefaultsMatchImplementation(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer func() { _ = eng.Close(context.Background()) }()

	seed, err := eng.IngestEpisode(ctx, EpisodeInput{
		SpaceID: "default",
		Kind:    "chat_message",
		Content: "We decided to keep raw Cypher internal.",
		Source:  SourceInput{Kind: "chat", ExternalRef: "thread-docs-contract"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	subject, err := eng.UpsertEntity(ctx, EntityInput{
		SpaceID:       "default",
		Type:          "Project",
		Namespace:     "default",
		CanonicalName: "Yeoul",
	})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		SpaceID:              "default",
		Predicate:            "DECIDED",
		SubjectID:            subject.ID,
		ValueText:            "keep raw Cypher internal",
		SupportingEpisodeIDs: []string{seed.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}

	// Provenance depth default is 8, not the 2 the reference used to claim.
	provenance, err := eng.Provenance(ctx, ProvenanceRequest{ID: fact.ID, Kind: "fact"})
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if provenance.Root.ID != fact.ID {
		t.Fatalf("provenance root = %q, want %q", provenance.Root.ID, fact.ID)
	}
	if len(provenance.Edges) == 0 {
		t.Fatal("provenance returned no edges for a supported fact")
	}

	queryAPI := readRepositoryDoc(t, "docs/05-api/query-api.md")
	if !strings.Contains(queryAPI, "`max_depth` defaults to `8`") {
		t.Fatal("docs/05-api/query-api.md must document the provenance depth default of 8")
	}

	// Predicates are a hard exclusion, so a non-matching predicate yields nothing.
	filtered, err := eng.Search(ctx, SearchRequest{
		QueryText:  "Cypher",
		Predicates: []string{"UNRELATED"},
	})
	if err != nil {
		t.Fatalf("search with predicate filter: %v", err)
	}
	for _, hit := range filtered.Hits {
		if hit.RecordID == fact.ID {
			t.Fatal("predicate filter behaved as a soft filter instead of a hard exclusion")
		}
	}
	if !strings.Contains(queryAPI, "hard exclusion") {
		t.Fatal("docs/05-api/query-api.md must describe predicates as a hard exclusion")
	}

	// Provenance edges are the emitted set, not the advertised DERIVED_FROM/SUPPORTS set.
	emitted := map[string]bool{}
	for _, edge := range provenance.Edges {
		emitted[edge.Type] = true
	}
	emittedEdgeTypes := []string{"ASSERTS", "SUPERSEDES", "FROM_SOURCE", "SUBJECT", "OBJECT"}
	for edgeType := range emitted {
		if !slices.Contains(emittedEdgeTypes, edgeType) {
			t.Fatalf("provenance emitted %q, which the reference does not list", edgeType)
		}
	}
	for _, unsupported := range []string{"DERIVED_FROM", "SUPPORTS"} {
		if emitted[unsupported] {
			t.Fatalf("provenance emitted %q, which the reference lists as unsupported", unsupported)
		}
	}
	if !strings.Contains(queryAPI, "`ASSERTS`") || !strings.Contains(queryAPI, "`FROM_SOURCE`") {
		t.Fatal("docs/05-api/query-api.md must list the provenance edges the engine emits")
	}
}

func readRepositoryDoc(t *testing.T, relativePath string) string {
	t.Helper()
	root := repositoryRoot(t)
	body, err := os.ReadFile(filepath.Join(root, relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(body)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root")
		}
		dir = parent
	}
}
