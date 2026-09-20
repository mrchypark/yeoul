package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// migrationDirectorySyncRecorder replaces the directory durability hook with
// one that records every sync and restores the previous hook with t.Cleanup.
func migrationDirectorySyncRecorder(t *testing.T) *[]string {
	t.Helper()
	previous := syncMigrationDirectory
	calls := &[]string{}
	syncMigrationDirectory = func(path string) error {
		*calls = append(*calls, path)
		return nil
	}
	t.Cleanup(func() { syncMigrationDirectory = previous })
	return calls
}

// migrationDirectorySyncFailure makes every directory sync fail with failure
// and restores the previous hook with t.Cleanup.
func migrationDirectorySyncFailure(t *testing.T, failure error) {
	t.Helper()
	previous := syncMigrationDirectory
	syncMigrationDirectory = func(string) error { return failure }
	t.Cleanup(func() { syncMigrationDirectory = previous })
}

// enableInProcessLegacyMigration pins the reader version so MigrateDatabase runs
// the in-process conversion instead of requiring the pinned external helper.
func enableInProcessLegacyMigration(t *testing.T) {
	t.Helper()
	previous := legacyMigrationReaderVersion
	legacyMigrationReaderVersion = "v0.13.1"
	t.Cleanup(func() { legacyMigrationReaderVersion = previous })
}

// writeLegacyDatabaseFixture creates a committed legacy Ladybug database plus a
// sidecar so the migration exercises the full backup move.
func writeLegacyDatabaseFixture(t *testing.T, databasePath string) {
	t.Helper()
	ctx := context.Background()
	legacy, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        databasePath,
		legacyLadybugWrites: true,
		CreateIfMissing:     true,
	})
	if err != nil {
		t.Fatalf("open legacy engine: %v", err)
	}
	if _, err := legacy.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-durability",
		Kind:    "note",
		Content: "durability fixture",
		Source:  SourceInput{Kind: "test", ExternalRef: "migration-durability"},
	}); err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}
	// The read-only Ladybug engine replays a non-empty .wal file, so the fixture
	// uses an empty but present sidecar to exercise the sibling move.
	if err := os.WriteFile(databasePath+".wal", []byte{}, 0o600); err != nil {
		t.Fatalf("write legacy WAL sidecar: %v", err)
	}
}

func TestMigrateDatabaseSyncsDirectoryTransitions(t *testing.T) {
	enableInProcessLegacyMigration(t)
	calls := migrationDirectorySyncRecorder(t)

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
	writeLegacyDatabaseFixture(t, dbPath)

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	if !result.Migrated {
		t.Fatalf("expected the migration to report success: %#v", result)
	}

	parent := filepath.Dir(dbPath)
	for index, call := range *calls {
		if call != parent {
			t.Fatalf("directory sync %d targeted %q, want %q", index, call, parent)
		}
	}
	// The success path has six namespace transitions, each of which must sync
	// the database's parent directory: the prepared marker publication, the
	// backup move, the backed-up marker publication, the staging install, the
	// installed marker publication, and the marker removal.
	if len(*calls) != 6 {
		t.Fatalf("expected 6 directory syncs on the migration success path, got %d", len(*calls))
	}
}

func TestMigrateDatabaseSurfacesDirectorySyncFailure(t *testing.T) {
	enableInProcessLegacyMigration(t)
	syncFailure := errors.New("injected directory sync failure")
	migrationDirectorySyncFailure(t, syncFailure)

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
	writeLegacyDatabaseFixture(t, dbPath)

	if _, err := MigrateDatabase(ctx, dbPath); !errors.Is(err, syncFailure) {
		t.Fatalf("expected the directory sync failure to surface, got %v", err)
	}
}

func TestRecoverDatabaseMigrationSurfacesDirectorySyncFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recover.ltdb")
	backupPath := dbPath + ".ladybug-backup-0006"
	stagingPath := dbPath + ".lattice-migrate-0006"
	if err := os.WriteFile(dbPath, []byte("installed"), 0o600); err != nil {
		t.Fatalf("write installed database: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhaseInstalled,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write installed marker: %v", err)
	}

	syncFailure := errors.New("injected directory sync failure")
	migrationDirectorySyncFailure(t, syncFailure)

	if err := recoverDatabaseMigration(dbPath); !errors.Is(err, syncFailure) {
		t.Fatalf("expected the directory sync failure to surface from recovery, got %v", err)
	}
}
