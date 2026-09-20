package yeoul

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// appendWALByte simulates a database left with pending native WAL work: the
// writable open replays and checkpoints that tail, which rewrites database
// files. A build that rejects the state version only after that writable open
// has already modified a database it cannot read.
func appendWALByte(t *testing.T, dbPath string) {
	t.Helper()
	walPath := filepath.Join(dbPath, "wal.log")
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read native WAL: %v", err)
	}
	if err := os.WriteFile(walPath, append(data, 0x00), 0o600); err != nil {
		t.Fatalf("append native WAL byte: %v", err)
	}
}

// TestLatticeOpenRejectsUnsupportedStateVersionBeforeWritableOpen proves the
// version is resolved through a read-only inspection before any writable handle
// exists. The fixture carries a pending native WAL tail, which a writable open
// would replay and checkpoint, so the database bytes are the evidence: a
// rejection that happens after the writable open leaves them changed.
func TestLatticeOpenRejectsUnsupportedStateVersionBeforeWritableOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "unsupported-wal.db")
	writeLatticeStateFixture(t, dbPath, "2", false, unknownFieldFixtureNodes())
	appendWALByte(t, dbPath)
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
