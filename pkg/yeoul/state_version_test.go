package yeoul

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	latticedb "github.com/mrchypark/latticedb-go"
)

// writeLatticeStateFixture creates a lattice database that carries the given
// application-state version and the given nodes, without going through the
// public open path. An empty version with omitVersion false writes an empty
// version value; omitVersion true leaves the version metadata absent.
func writeLatticeStateFixture(t *testing.T, dbPath, version string, omitVersion bool, nodes []latticedb.CreateNodeOptions) {
	t.Helper()
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{Create: true})
	if err != nil {
		t.Fatalf("create lattice fixture: %v", err)
	}
	if err := db.Update(func(tx *latticedb.Tx) error {
		if !omitVersion {
			if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(version)); err != nil {
				return err
			}
		}
		for _, node := range nodes {
			if _, err := tx.CreateNode(node); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write lattice fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close lattice fixture: %v", err)
	}
}

// unknownFieldFixtureNodes holds one record whose payload carries a field this
// build does not know. A version rewrite drops that field on the next record
// modification, so the fixture is what a failed open must leave untouched.
func unknownFieldFixtureNodes() []latticedb.CreateNodeOptions {
	return []latticedb.CreateNodeOptions{{
		Labels: []string{"Episode"},
		Properties: map[string]any{
			"id":      "ep-unknown-field",
			"payload": `{"id":"ep-unknown-field","kind":"note","content":"kept","unknown_future_field":{"nested":[1,2,3]}}`,
		},
	}}
}

// hashDatabaseTree hashes every file under the lattice database directory, so a
// failed open can be compared byte for byte against the state before it.
func hashDatabaseTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			fmt.Fprintf(h, "d:%s\n", rel)
			return nil
		}
		// Native lock files are markers for an OS lock, not database bytes, and a
		// live writer holds them locked. Windows refuses even a read of a byte
		// range another handle has locked, so hashing them would fail the
		// comparison before the behavior under test ran. The other database-tree
		// helpers in this package skip them for the same reason.
		if strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "f:%s:%x\n", rel, sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatalf("hash database tree %s: %v", root, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func assertUnsupportedStateVersionError(t *testing.T, err error, found string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an unsupported application-state version error")
	}
	if !errors.Is(err, errUnsupportedStateVersion) {
		t.Fatalf("expected the unsupported state version sentinel, got %v", err)
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a typed yeoul error, got %T", err)
	}
	if yeoulErr.Code != ErrNotSupported {
		t.Fatalf("unexpected error code: %v", yeoulErr.Code)
	}
	if yeoulErr.Details["found_version"] != found {
		t.Fatalf("unexpected found_version: %v", yeoulErr.Details["found_version"])
	}
	if yeoulErr.Details["supported_version"] != currentStateVersion {
		t.Fatalf("unexpected supported_version: %v", yeoulErr.Details["supported_version"])
	}
}

func TestLatticeOpenRejectsUnsupportedStateVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		omitVersion bool
		found       string
	}{
		{name: "newer version", version: "2", found: "2"},
		{name: "zero version", version: "0", found: "0"},
		{name: "negative version", version: "-1", found: "-1"},
		{name: "non-integer version", version: "one", found: "one"},
		{name: "empty version value", version: "", found: ""},
		{name: "missing version", omitVersion: true, found: "missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "unsupported.db")
			writeLatticeStateFixture(t, dbPath, test.version, test.omitVersion, unknownFieldFixtureNodes())
			before := hashDatabaseTree(t, dbPath)

			eng, err := Open(context.Background(), Config{DatabasePath: dbPath})
			assertUnsupportedStateVersionError(t, err, test.found)
			if eng != nil {
				t.Fatal("expected no engine for an unsupported state version")
			}
			if after := hashDatabaseTree(t, dbPath); after != before {
				t.Fatalf("failed open changed the database: before=%s after=%s", before, after)
			}
		})
	}
}

func TestLatticeReadOnlyOpenRejectsUnsupportedStateVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "unsupported-read-only.db")
	writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
	before := hashDatabaseTree(t, dbPath)

	eng, err := Open(context.Background(), Config{DatabasePath: dbPath, ReadOnly: true})
	assertUnsupportedStateVersionError(t, err, "2")
	if eng != nil {
		t.Fatal("expected no engine for an unsupported state version")
	}
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("failed read-only open changed the database: before=%s after=%s", before, after)
	}
}

func TestLatticeOpenAcceptsSupportedStateVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "supported.db")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open supported engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "supported state version",
		Source:  SourceInput{Kind: "test", ExternalRef: "supported-version"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	snapshot, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot engine: %v", err)
	}
	if snapshot.Version != currentStateVersion {
		t.Fatalf("unexpected snapshot version: %d", snapshot.Version)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen supported engine: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()
	got, err := reopened.GetEpisode(ctx, episode.EpisodeID)
	if err != nil {
		t.Fatalf("get persisted episode: %v", err)
	}
	if got.Content != "supported state version" {
		t.Fatalf("unexpected persisted episode: %#v", got)
	}
}

func TestLatticeSnapshotCarriesSupportedStateVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "snapshot-version.db")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "snapshot version",
		Source:  SourceInput{Kind: "test", ExternalRef: "snapshot-version"},
	}); err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("open raw lattice database: %v", err)
	}
	defer db.Close()
	var persisted string
	if err := db.View(func(tx *latticedb.Tx) error {
		value, ok, err := tx.GetAppMetadata([]byte(latticeMetaVersion))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("state version metadata is missing")
		}
		persisted = string(value)
		return nil
	}); err != nil {
		t.Fatalf("read persisted state version: %v", err)
	}
	if persisted != strconv.Itoa(currentStateVersion) {
		t.Fatalf("snapshot wrote version %q, want %d", persisted, currentStateVersion)
	}
}
