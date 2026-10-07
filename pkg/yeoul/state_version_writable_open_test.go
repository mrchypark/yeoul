package yeoul

import (
	"context"
	"path/filepath"
	"testing"
)

// TestLatticeOpenRejectsUnsupportedStateVersionBeforeWritableOpen proves the
// version is resolved through a read-only inspection before any writable handle
// exists. The database tree must remain unchanged when the version is rejected.
func TestLatticeOpenRejectsUnsupportedStateVersionBeforeWritableOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "unsupported-wal.db")
	writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
	before := hashDatabaseTree(t, dbPath)

	eng, err := Open(context.Background(), Config{DatabasePath: dbPath})
	assertUnsupportedStateVersionError(t, err, "2")
	if eng != nil {
		t.Fatal("expected no engine for an unsupported state version")
	}
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("rejected open changed the database: before=%s after=%s", before, after)
	}
}
