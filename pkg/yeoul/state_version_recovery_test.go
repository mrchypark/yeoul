package yeoul

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFailsClosedOnInterruptedMigrationMarker(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "pending.ltdb")
	markerPath := databaseMigrationMarkerPath(dbPath)
	backupPath := dbPath + ".backup-test"
	stagingPath := dbPath + ".lattice-migrate-test"
	marker := []byte(`{"phase":"backed_up","database_path":"pending.ltdb"}`)
	backup := []byte("database backup fixture")
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	writeLatticeStateFixture(t, stagingPath, "1", false, nil)
	stagingBefore := hashDatabaseTree(t, stagingPath)

	if _, err := Open(context.Background(), Config{DatabasePath: dbPath, CreateIfMissing: true}); err == nil {
		t.Fatal("expected an unfinished migration marker to fail closed")
	}
	for path, want := range map[string][]byte{markerPath: marker, backupPath: backup} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved artifact %s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("artifact %s changed", path)
		}
	}
	if got := hashDatabaseTree(t, stagingPath); got != stagingBefore {
		t.Fatalf("staging database changed: before=%s after=%s", stagingBefore, got)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical path should remain absent, got %v", err)
	}
}

func TestOpenFailsClosedWithoutChangingExistingLatticeAtMigrationMarker(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "existing.ltdb")
	writeLatticeStateFixture(t, dbPath, "1", false, unknownFieldFixtureNodes())
	before := hashDatabaseTree(t, dbPath)
	markerPath := databaseMigrationMarkerPath(dbPath)
	marker := []byte(`{"phase":"prepared"}`)
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), Config{DatabasePath: dbPath}); err == nil {
		t.Fatal("expected an existing database with a migration marker to fail closed")
	}
	if after := hashDatabaseTree(t, dbPath); after != before {
		t.Fatalf("existing Lattice database changed: before=%s after=%s", before, after)
	}
	got, err := os.ReadFile(markerPath)
	if err != nil || !bytes.Equal(got, marker) {
		t.Fatalf("migration marker changed: content=%q err=%v", got, err)
	}
}
