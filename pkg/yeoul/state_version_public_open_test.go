package yeoul

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	latticedb "github.com/mrchypark/latticedb-go"
)

func TestPublicOpenPreservesLatticeLogicalState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "original.ltdb")
	writeLatticeStateFixture(t, dbPath, "1", false, []latticedb.CreateNodeOptions{{
		Labels: []string{"Episode"},
		Properties: map[string]any{
			"id":      "ep-original",
			"payload": `{"id":"ep-original","kind":"note","content":"original state"}`,
		},
	}})
	readState := func() persistedState {
		t.Helper()
		store, err := newLatticeStore(Config{DatabasePath: dbPath, ReadOnly: true})
		if err != nil {
			t.Fatalf("open database read-only: %v", err)
		}
		state, err := store.Load()
		if err != nil {
			_ = store.Close()
			t.Fatalf("load logical state: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		return *state
	}
	before := readState()
	if before.Version != currentStateVersion || len(before.Episodes) != 1 {
		t.Fatalf("expected a supported nonempty Lattice state, got %#v", before)
	}

	eng, err := Open(context.Background(), Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("open Lattice database: %v", err)
	}
	if err := eng.Close(context.Background()); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	after := readState()
	if after.Version != before.Version || after.Sequence != before.Sequence ||
		!reflect.DeepEqual(after.Sources, before.Sources) ||
		!reflect.DeepEqual(after.Episodes, before.Episodes) ||
		!reflect.DeepEqual(after.Entities, before.Entities) ||
		!reflect.DeepEqual(after.Facts, before.Facts) ||
		!reflect.DeepEqual(after.FactRevisions, before.FactRevisions) ||
		!reflect.DeepEqual(after.EntityRevisions, before.EntityRevisions) {
		t.Fatalf("opening changed logical state: before=%#v after=%#v", before, after)
	}
}
