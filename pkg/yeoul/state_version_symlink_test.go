package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkedDatabasePaths returns one database reached through two spellings:
// the database itself and a symlink that points at it. The native engine
// resolves the symlink before it locks the database, so both spellings are one
// database to it, and every ownership or marker lookup in Yeoul has to agree.
//
// The link is what makes the two spellings disagree: an ownership file is a
// sibling of the name it is derived from, so the linked spelling would place it
// beside the link while the engine locks the database behind it.
func symlinkedDatabasePaths(t *testing.T) (realPath, linkPath string) {
	t.Helper()
	dir := t.TempDir()
	realPath = filepath.Join(dir, "real.ltdb")
	if err := os.MkdirAll(realPath, 0o755); err != nil {
		t.Fatalf("create real database directory: %v", err)
	}
	linkPath = filepath.Join(dir, "linked.ltdb")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("link database: %v", err)
	}
	return realPath, linkPath
}

// TestDatabaseOwnershipPathResolvesDirectorySymlinks pins the property the
// ownership lock depends on: two spellings of one database have to name one
// ownership file. Otherwise a second opener takes a lock the first owner cannot
// see, and the exclusion the writable open relies on does not exist.
func TestDatabaseOwnershipPathResolvesDirectorySymlinks(t *testing.T) {
	realPath, linkPath := symlinkedDatabasePaths(t)

	resolvedLink, err := resolveDatabasePathAliases(linkPath)
	if err != nil {
		t.Fatalf("resolve linked database path: %v", err)
	}
	resolvedReal, err := resolveDatabasePathAliases(realPath)
	if err != nil {
		t.Fatalf("resolve real database path: %v", err)
	}
	if databaseOwnershipPath(resolvedLink) != databaseOwnershipPath(resolvedReal) {
		t.Fatalf("one database resolved to two ownership files: link=%q real=%q",
			databaseOwnershipPath(resolvedLink), databaseOwnershipPath(resolvedReal))
	}
	if databaseMigrationMarkerPath(resolvedLink) != databaseMigrationMarkerPath(resolvedReal) {
		t.Fatalf("one database resolved to two migration markers: link=%q real=%q",
			databaseMigrationMarkerPath(resolvedLink), databaseMigrationMarkerPath(resolvedReal))
	}
	if resolvedLink != resolvedReal {
		t.Fatalf("expected both spellings to resolve to one database path: link=%q real=%q", resolvedLink, resolvedReal)
	}
}

// TestOpenThroughASymlinkHonoursTheOwnershipOfTheRealPath is the behavioural
// half: an exclusive owner of the real path has to keep a second opener out
// even when that opener arrives through a symlinked spelling. The ownership
// lock is what makes the version inspection and the writable open one critical
// section, so an opener that misses the lock can hand a database another owner
// is replacing to a writable open.
func TestOpenThroughASymlinkHonoursTheOwnershipOfTheRealPath(t *testing.T) {
	ctx := context.Background()
	realPath, linkPath := symlinkedDatabasePaths(t)
	writeLatticeStateFixture(t, realPath, "1", false, nil)

	exclusive, err := acquireDatabaseOwnership(realPath, true)
	if err != nil {
		t.Fatalf("acquire exclusive ownership: %v", err)
	}
	defer func() { _ = exclusive.Release() }()

	eng, err := Open(ctx, Config{DatabasePath: linkPath})
	if eng != nil {
		_ = eng.Close(ctx)
		t.Fatal("expected an open through a symlink to honour the ownership of the real path")
	}
	if err == nil || !strings.Contains(err.Error(), "database migration is in progress") {
		t.Fatalf("expected the ownership refusal, got %v", err)
	}
}

// TestOpenThroughASymlinkRejectsAnUnsupportedDatabase covers the version
// decision across the same aliasing: the inspection and the writable open have
// to act on the database the caller named, whichever spelling reached it, so a
// database this build cannot read is refused and left unchanged.
func TestOpenThroughASymlinkRejectsAnUnsupportedDatabase(t *testing.T) {
	realPath, linkPath := symlinkedDatabasePaths(t)
	writeLatticeStateFixture(t, realPath, "2", false, unknownFieldFixtureNodes())
	before := hashDatabaseTree(t, realPath)

	eng, err := Open(context.Background(), Config{DatabasePath: linkPath})
	if eng != nil {
		_ = eng.Close(context.Background())
		t.Fatal("expected no engine for an unsupported state version")
	}
	assertUnsupportedStateVersionError(t, err, "2")
	if after := hashDatabaseTree(t, realPath); after != before {
		t.Fatalf("the refusal through a symlink changed the database: before=%s after=%s", before, after)
	}
}

// TestOpenThroughASymlinkCreatesTheDatabaseAtTheRealPath proves the creation
// path agrees with the resolution as well: the database a symlinked spelling
// creates is the database the real spelling opens, not a second one beside the
// link.
func TestOpenThroughASymlinkCreatesTheDatabaseAtTheRealPath(t *testing.T) {
	ctx := context.Background()
	realPath, linkPath := symlinkedDatabasePaths(t)

	eng, err := Open(ctx, Config{DatabasePath: linkPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create through a symlinked directory: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	assertLatticeDatabaseInitialized(t, realPath)
	// The ownership file has to be the one beside the resolved database. The
	// linked spelling reaches the same file, so the count is what proves there
	// is one ownership namespace and not two.
	resolvedReal, err := resolveDatabasePathAliases(realPath)
	if err != nil {
		t.Fatalf("resolve real database path: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(resolvedReal))
	if err != nil {
		t.Fatalf("read database directory: %v", err)
	}
	ownershipFiles := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), databaseOwnershipSuffix) {
			ownershipFiles++
		}
	}
	if ownershipFiles != 1 {
		t.Fatalf("expected one ownership file for one database, found %d", ownershipFiles)
	}

	reopened, err := Open(ctx, Config{DatabasePath: realPath})
	if err != nil {
		t.Fatalf("expected the real path to open the database the link created: %v", err)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
}

// TestMigrateDatabaseResolvesDirectorySymlinks pins the migration side of the
// same property: the ownership lock the migration takes, the marker it writes,
// and the paths it moves have to name the database the caller pointed at, so a
// migration started through a symlink is not a second migration namespace.
func TestMigrateDatabaseResolvesDirectorySymlinks(t *testing.T) {
	realPath, linkPath := symlinkedDatabasePaths(t)
	writeLatticeStateFixture(t, realPath, "1", false, nil)

	result, err := MigrateDatabase(context.Background(), linkPath)
	if err != nil {
		t.Fatalf("migrate through a symlinked directory: %v", err)
	}
	// macOS resolves the temporary directory itself through a symlink, so the
	// expected path is the resolved one rather than the spelling the test built.
	resolvedReal, err := resolveDatabasePathAliases(realPath)
	if err != nil {
		t.Fatalf("resolve real database path: %v", err)
	}
	if result.DatabasePath != resolvedReal {
		t.Fatalf("expected the migration to report the resolved database path: got %q want %q", result.DatabasePath, resolvedReal)
	}
	if result.SourceDriver != string(StorageDriverLattice) || result.TargetDriver != string(StorageDriverLattice) {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(realPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no migration marker beside the resolved database, got %v", err)
	}
}
