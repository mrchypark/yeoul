package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	latticedb "github.com/mrchypark/latticedb-go"
)

// holdWritableLatticeHandle opens a raw native writable handle on a database,
// which is what a competing writer of any build looks like to this process: the
// native engine refuses every other open until the handle is closed.
func holdWritableLatticeHandle(t *testing.T, dbPath string) *latticedb.DB {
	t.Helper()
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{})
	if err != nil {
		t.Fatalf("hold writable lattice handle: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// installUnreadableState writes an application-state version this build cannot
// read through a writer that already owns the database, which is how a database
// this build must refuse appears while its owner is live.
func installUnreadableState(t *testing.T, db *latticedb.DB) error {
	t.Helper()
	return db.Update(func(tx *latticedb.Tx) error {
		if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte("2")); err != nil {
			return err
		}
		_, err := tx.CreateNode(unknownFieldFixtureNodes()[0])
		return err
	})
}

// TestLatticeOpenRefusesUninspectedDatabaseHeldByAWriter covers the contended
// inspection: a database this build cannot inspect because another writer holds
// it must not reach a writable open. The version is unknown in that window, so a
// writable open could run native recovery on state this build cannot read.
func TestLatticeOpenRefusesUninspectedDatabaseHeldByAWriter(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "contended.ltdb")
	writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
	appendWALByte(t, dbPath)

	// The competing writer replays the pending native WAL as it opens, so the
	// bytes to compare against are the ones that exist once it is in place.
	writer := holdWritableLatticeHandle(t, dbPath)
	before := hashDatabaseTree(t, dbPath)

	eng, err := Open(ctx, Config{DatabasePath: dbPath})
	if err == nil {
		_ = eng.Close(ctx)
		t.Fatal("expected an open of a database held by another writer to be refused")
	}
	if !errors.Is(err, errLatticeStateVersionUnestablished) {
		t.Fatalf("expected the uninspected-version refusal, got %v", err)
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) || yeoulErr.Code != ErrStorageFailed {
		t.Fatalf("expected a storage failure for an uninspected database, got %v", err)
	}
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("a refused open changed a database it could not inspect: before=%s after=%s", before, after)
	}

	// The refused open must not have consumed anything the version check needs:
	// once the writer is gone, the same database is still rejected as an
	// unsupported version rather than opened or converted.
	if err := writer.Close(); err != nil {
		t.Fatalf("close competing writer: %v", err)
	}
	eng, err = Open(ctx, Config{DatabasePath: dbPath})
	if eng != nil {
		_ = eng.Close(ctx)
		t.Fatal("expected no engine for an unsupported state version")
	}
	assertUnsupportedStateVersionError(t, err, "2")
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("the rejection after the writer left changed the database: before=%s after=%s", before, after)
	}
}

// TestLatticeOpenReinspectsWhenAWriterFillsTheCreationWindow covers the second
// half of the gap: the inspection finds no persisted version, so the writable
// open is about to create or initialize the database, and a competing writer
// installs an unreadable state in that window. The version is inspected again
// under the open's own exclusive ownership, so the database is rejected instead
// of being handed to a writable open.
func TestLatticeOpenReinspectsWhenAWriterFillsTheCreationWindow(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		// fill prepares the database the open will inspect before the window.
		fill func(t *testing.T, dbPath string)
		// install writes the unreadable state inside the window and reports
		// whether it also has to hold the database for the writable open.
		install func(t *testing.T, dbPath string) bool
		// keepBytes asserts that the refused open left the installed database
		// byte for byte as it was, which only the inspection before the
		// writable open can guarantee. It is skipped when the competing writer
		// keeps its own handle, because that writer rewrites the database.
		keepBytes bool
	}{
		{
			name: "database appears in the window",
			fill: func(t *testing.T, dbPath string) {},
			install: func(t *testing.T, dbPath string) bool {
				writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
				// A pending native WAL tail is what a writable open would
				// replay and checkpoint, so the database bytes show whether the
				// version was resolved before that open.
				appendWALByte(t, dbPath)
				return false
			},
			keepBytes: true,
		},
		{
			name: "empty database is filled in the window",
			fill: func(t *testing.T, dbPath string) {
				empty, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
				if err != nil {
					t.Fatalf("create empty database: %v", err)
				}
				if err := empty.Close(ctx); err != nil {
					t.Fatalf("close empty database: %v", err)
				}
			},
			install: func(t *testing.T, dbPath string) bool {
				// The competing writer keeps its handle, so the database is
				// both unreadable and unreachable for the writable open.
				writer := holdWritableLatticeHandle(t, dbPath)
				if err := installUnreadableState(t, writer); err != nil {
					t.Errorf("install unreadable state in the creation window: %v", err)
				}
				return true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "window.ltdb")
			test.fill(t, dbPath)

			installed := false
			held := false
			var installedBytes string
			previous := writableOpenBarrier
			writableOpenBarrier = func() {
				if installed {
					return
				}
				installed = true
				held = test.install(t, dbPath)
				installedBytes = hashDatabaseTree(t, dbPath)
			}
			t.Cleanup(func() { writableOpenBarrier = previous })

			eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
			if eng != nil {
				_ = eng.Close(ctx)
				t.Fatal("expected no engine for a database filled with an unsupported state version")
			}
			if !installed {
				t.Fatal("expected the creation window to be exercised")
			}
			if held {
				// A database another writer holds is refused as uninspected
				// rather than as an unsupported version, and it must not have
				// been converted or overwritten on the way out.
				if !errors.Is(err, errLatticeStateVersionUnestablished) {
					t.Fatalf("expected the uninspected-version refusal, got %v", err)
				}
				return
			}
			assertUnsupportedStateVersionError(t, err, "2")
			if test.keepBytes {
				if after := hashDatabaseTree(t, dbPath); after != installedBytes {
					t.Fatalf("a refused open recovered a database it had not inspected: before=%s after=%s", installedBytes, after)
				}
			}
		})
	}
}
