package yeoul

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func createOwnedLegacyDatabase(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		CreateIfMissing:     true,
		legacyLadybugWrites: true,
	})
	if err != nil {
		t.Fatalf("create legacy database: %v", err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-owned",
		Kind:    "note",
		Content: "owned",
		Source:  SourceInput{Kind: "test", ExternalRef: "owned"},
	}); err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}
}

// TestMigrationLockBlocksConcurrentOwners verifies that a migration holds the
// per-database lock for its whole duration: opens and further migrations are
// refused while it is held and succeed once it is released.
func TestMigrationLockBlocksConcurrentOwners(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "owned.lbug")
	createOwnedLegacyDatabase(t, dbPath)

	release, err := acquireLegacyMigrationLock(dbPath)
	if err != nil {
		t.Fatalf("acquire migration lock: %v", err)
	}
	defer func() { _ = release() }()

	if _, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true}); err == nil {
		t.Fatal("expected an open to be refused while a migration lock is held")
	} else {
		t.Logf("open refused with: %v", err)
	}
	if _, err := MigrateDatabase(ctx, dbPath); err == nil {
		t.Fatal("expected a second migration to be refused while the lock is held")
	} else {
		t.Logf("second migration refused with: %v", err)
	}

	if err := release(); err != nil {
		t.Fatalf("release migration lock: %v", err)
	}
	opened, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("expected the database to open after the lock was released: %v", err)
	}
	if err := opened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
}

// TestStaleMigrationLockIsReclaimed verifies that a lock left behind by a dead
// process does not block later opens.
func TestStaleMigrationLockIsReclaimed(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "stale.lbug")
	createOwnedLegacyDatabase(t, dbPath)

	if err := os.WriteFile(legacyMigrationLockPath(dbPath), []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write stale migration lock: %v", err)
	}
	opened, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("expected the stale lock to be reclaimed: %v", err)
	}
	if err := opened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
	if _, err := os.Stat(legacyMigrationLockPath(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("expected the stale lock to be removed, got %v", err)
	}
}
