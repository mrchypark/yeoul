package yeoul

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	latticedb "github.com/mrchypark/latticedb-go"
)

func TestLatticeIsDefaultAndPersistsGraph(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.db")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open default engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind: "note", Content: "LatticeDB default persistence",
		Source: SourceInput{Kind: "test", ExternalRef: "lattice-default"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		Predicate: "USES_STORAGE_ENGINE", SubjectID: entity.ID, ValueText: "LatticeDB",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen default engine: %v", err)
	}
	got, err := reopened.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get persisted fact: %v", err)
	}
	if got.SubjectID != entity.ID || got.ValueText != "LatticeDB" {
		t.Fatalf("unexpected persisted fact: %#v", got)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
}

func TestLatticeDatabaseCanUseLbugSuffix(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "lattice.lbug")
	eng, err := Open(context.Background(), Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open Lattice database with .lbug suffix: %v", err)
	}
	if err := eng.Close(context.Background()); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("expected a Lattice directory at the .lbug path, info=%v err=%v", info, err)
	}
	reopened, err := Open(context.Background(), Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen Lattice directory with .lbug suffix: %v", err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatalf("close reopened database: %v", err)
	}
}

func TestLegacyLadybugRegularFileIsRefusedWithoutModification(t *testing.T) {
	fixture, err := os.Open("testdata/v022-lifecycle.lbug.gz")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := gzip.NewReader(fixture)
	if err != nil {
		_ = fixture.Close()
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(compressed)
	closeErr := errors.Join(compressed.Close(), fixture.Close())
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), Config{DatabasePath: dbPath, CreateIfMissing: true}); err == nil {
		t.Fatal("expected the legacy regular file to be refused")
	}
	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("refused legacy file was modified")
	}
}

func TestOpenRejectsUnsupportedExplicitStorageDriver(t *testing.T) {
	_, err := Open(context.Background(), Config{DatabasePath: filepath.Join(t.TempDir(), "unsupported.db"), Driver: StorageDriver("unsupported")})
	structured := unwrapYeoulError(err)
	if structured == nil || structured.Code != ErrConfigInvalid {
		t.Fatalf("expected unsupported driver to return ErrConfigInvalid, got %v", err)
	}
}

func TestLatticeLoadRejectsCorruptRecordIdentity(t *testing.T) {
	tests := []struct {
		name  string
		nodes []map[string]any
	}{
		{"payload id mismatch", []map[string]any{{"id": "ep-indexed", "payload": `{"id":"ep-payload","kind":"note","content":"corrupt"}`}}},
		{"duplicate indexed id", []map[string]any{
			{"id": "ep-duplicate", "payload": `{"id":"ep-duplicate","kind":"note","content":"one"}`},
			{"id": "ep-duplicate", "payload": `{"id":"ep-duplicate","kind":"note","content":"two"}`},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "corrupt.db")
			db, err := latticedb.Open(dbPath, latticedb.OpenOptions{Create: true})
			if err != nil {
				t.Fatalf("create lattice database: %v", err)
			}
			if err := db.Update(func(tx *latticedb.Tx) error {
				if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(currentStateVersion))); err != nil {
					return err
				}
				for _, properties := range test.nodes {
					if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Episode"}, Properties: properties}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatalf("write corrupt lattice database: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close corrupt lattice database: %v", err)
			}
			if _, err := Open(context.Background(), Config{Driver: StorageDriverLattice, DatabasePath: dbPath, ReadOnly: true}); err == nil {
				t.Fatal("expected corrupt lattice record identity to be rejected")
			}
		})
	}
}

func TestLatticeOpenRejectsCrossKindRecordIDCollision(t *testing.T) {
	const sharedID = "shared-cross-kind-id"
	dbPath := filepath.Join(t.TempDir(), "collision.db")
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{Create: true})
	if err != nil {
		t.Fatalf("create lattice database: %v", err)
	}
	if err := db.Update(func(tx *latticedb.Tx) error {
		if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(currentStateVersion))); err != nil {
			return err
		}
		if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Entity"}, Properties: map[string]any{
			"id": sharedID, "payload": `{"id":"shared-cross-kind-id","type":"Thing","canonical_name":"Shared"}`,
		}}); err != nil {
			return err
		}
		if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Episode"}, Properties: map[string]any{
			"id": sharedID, "payload": `{"id":"shared-cross-kind-id","kind":"note","content":"shared"}`,
		}}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("write colliding lattice database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close colliding lattice database: %v", err)
	}

	_, err = Open(context.Background(), Config{Driver: StorageDriverLattice, DatabasePath: dbPath, ReadOnly: true})
	structured := unwrapYeoulError(err)
	if structured == nil || structured.Code != ErrStorageFailed || structured.Details["id"] != sharedID {
		t.Fatalf("expected a structured cross-kind collision error, got %v", err)
	}
	kinds, ok := structured.Details["kinds"].([]string)
	if !ok || !slices.Contains(kinds, kindEntity) || !slices.Contains(kinds, kindEpisode) {
		t.Fatalf("expected both record kinds in error details, got %#v", structured.Details)
	}
}
