package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// prepareUninitializedDirectory creates the directory a deployment may
// provision ahead of the first open: it exists, but it holds no database yet.
func prepareUninitializedDirectory(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create provisioned directory: %v", err)
	}
}

// assertLatticeDatabaseInitialized checks that a writable open created the
// native database inside the directory it was given.
func assertLatticeDatabaseInitialized(t *testing.T, dbPath string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dbPath, "state.json")); err != nil {
		t.Fatalf("expected the writable open to initialize the database: %v", err)
	}
}

// assertUnestablishedStateVersionError checks that an open refused a database
// whose version it could not establish, which is the refusal that must not fall
// through to the legacy conversion.
func assertUnestablishedStateVersionError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the open to refuse a database whose state version it could not establish")
	}
	if !errors.Is(err, errLatticeStateVersionUnestablished) {
		t.Fatalf("expected the uninspected-version refusal, got %v", err)
	}
}

// TestPublicOpenCreatesInAnExistingEmptyDirectory covers the creation case the
// inspection has to let through: the path is already a directory, so a stat
// check passes, but it holds no native database yet. The native engine reports
// that as a missing file rather than as an empty database, so an inspection that
// only distinguishes "exists" from "missing" refuses the creation it was
// supposed to allow. Provisioning can create the directory ahead of the first
// open, and an interrupted creation can leave it behind, so both have to open.
func TestPublicOpenCreatesInAnExistingEmptyDirectory(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		prep   func(t *testing.T, dbPath string)
		driver StorageDriver
	}{
		{
			name: "existing empty directory",
			prep: func(t *testing.T, dbPath string) {
				t.Helper()
				prepareUninitializedDirectory(t, dbPath)
			},
		},
		{
			name: "directory holding only the native lock artifact",
			prep: func(t *testing.T, dbPath string) {
				t.Helper()
				prepareUninitializedDirectory(t, dbPath)
				if err := os.WriteFile(filepath.Join(dbPath, "state.json.lock"), nil, 0o600); err != nil {
					t.Fatalf("write native lock artifact: %v", err)
				}
			},
		},
		{
			name:   "existing empty directory with an explicit lattice driver",
			driver: StorageDriverLattice,
			prep: func(t *testing.T, dbPath string) {
				t.Helper()
				prepareUninitializedDirectory(t, dbPath)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "provisioned.ltdb")
			test.prep(t, dbPath)

			eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true, Driver: test.driver})
			if err != nil {
				t.Fatalf("expected a public open to create a database in a provisioned directory: %v", err)
			}
			if err := eng.Close(ctx); err != nil {
				t.Fatalf("close engine: %v", err)
			}
			assertLatticeDatabaseInitialized(t, dbPath)

			// The created database is a database this build can read, so the
			// second open is the ordinary path and not another creation.
			reopened, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true, Driver: test.driver})
			if err != nil {
				t.Fatalf("expected the created database to reopen: %v", err)
			}
			if err := reopened.Close(ctx); err != nil {
				t.Fatalf("close reopened engine: %v", err)
			}
		})
	}
}

// TestLatticeOpenRefusesUninitializedDirectoryWithoutCreationPolicy is the
// control for the creation case above: the same directory without the creation
// policy is not a database this build may initialize, and the refusal has to
// survive as the uninspected-version error so the open does not fall through to
// a legacy conversion of a directory that holds no legacy database.
func TestLatticeOpenRefusesUninitializedDirectoryWithoutCreationPolicy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "provisioned.ltdb")
	prepareUninitializedDirectory(t, dbPath)

	_, err := Open(context.Background(), Config{DatabasePath: dbPath})
	assertUnestablishedStateVersionError(t, err)
}

// TestLatticeOpenRefusesInitializedDatabaseInAnEmptyLookingDirectory proves the
// creation case cannot be claimed by a database that already exists: a directory
// that holds a native database under an unreadable application-state version
// looks empty to a stat-based check only when that check looks at the wrong
// thing, and it must stay rejected and byte-for-byte unchanged.
func TestLatticeOpenRefusesInitializedDatabaseInAnEmptyLookingDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "unsupported.ltdb")
	writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
	before := hashDatabaseTree(t, dbPath)

	eng, err := Open(context.Background(), Config{DatabasePath: dbPath, CreateIfMissing: true})
	if eng != nil {
		_ = eng.Close(context.Background())
		t.Fatal("expected no engine for an unsupported state version")
	}
	assertUnsupportedStateVersionError(t, err, "2")
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("the refused creation changed an existing database: before=%s after=%s", before, after)
	}
}

// TestLatticeInspectionAcceptsOnlyTheCreationItMayInitialize checks the
// inspection decision itself, so the creation policy is not inferred from a
// later failure. The read-only policy must refuse the uninitialized directory
// and the creation policy must accept it, and a database this build cannot read
// must stay refused under both.
func TestLatticeInspectionAcceptsOnlyTheCreationItMayInitialize(t *testing.T) {
	provisioned := filepath.Join(t.TempDir(), "provisioned.ltdb")
	prepareUninitializedDirectory(t, provisioned)
	assertUnestablishedStateVersionError(t, inspectStateVersionBeforeWritableOpen(provisioned, false))
	if err := inspectStateVersionBeforeWritableOpen(provisioned, true); err != nil {
		t.Fatalf("expected the creation policy to accept the directory it will initialize: %v", err)
	}

	unsupported := filepath.Join(t.TempDir(), "unsupported.ltdb")
	writeLatticeStateFixture(t, unsupported, "2", false, unknownFieldFixtureNodes())
	for _, createIfMissing := range []bool{false, true} {
		err := inspectStateVersionBeforeWritableOpen(unsupported, createIfMissing)
		assertUnsupportedStateVersionError(t, err, "2")
	}
}
