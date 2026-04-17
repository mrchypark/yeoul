package ladybug

import (
	"path/filepath"
	"testing"
)

func TestInMemorySmoke(t *testing.T) {
	store, err := OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory store: %v", err)
	}
	defer store.Close()

	queries := []string{
		"CREATE NODE TABLE Project(id STRING PRIMARY KEY, name STRING)",
		"CREATE (p:Project {id: 'project:yeoul', name: 'Yeoul'})",
		"MATCH (p:Project) RETURN p.name ORDER BY p.name",
	}
	if _, err := store.Query(queries[0]); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := store.Query(queries[1]); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	result, err := store.Query(queries[2])
	if err != nil {
		t.Fatalf("select node: %v", err)
	}
	defer result.Close()

	if !result.HasNext() {
		t.Fatal("expected at least one row")
	}
	tuple, err := result.Next()
	if err != nil {
		t.Fatalf("next tuple: %v", err)
	}
	value, err := tuple.GetValue(0)
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if got, ok := value.(string); !ok || got != "Yeoul" {
		t.Fatalf("unexpected value: %#v", value)
	}
}

func TestOnDiskReopenPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stage0-smoke.lbug")

	store, err := Open(dbPath, false)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if _, err := store.Query("CREATE NODE TABLE Decision(id STRING PRIMARY KEY, title STRING)"); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := store.Query("CREATE (d:Decision {id: 'dec-01', title: 'Keep raw Cypher internal'})"); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	store.Close()

	reopened, err := Open(dbPath, false)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()

	result, err := reopened.Query("MATCH (d:Decision {id: 'dec-01'}) RETURN d.title")
	if err != nil {
		t.Fatalf("query reopened store: %v", err)
	}
	defer result.Close()

	if !result.HasNext() {
		t.Fatal("expected persisted row")
	}
	tuple, err := result.Next()
	if err != nil {
		t.Fatalf("next tuple: %v", err)
	}
	value, err := tuple.GetValue(0)
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if got, ok := value.(string); !ok || got != "Keep raw Cypher internal" {
		t.Fatalf("unexpected persisted value: %#v", value)
	}
}
