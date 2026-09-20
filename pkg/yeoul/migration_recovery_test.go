package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// prepareInterruptedMigration reproduces the crash window between the source
// backup rename and the staging install: only the migration marker, the backup
// directory, and the staging database exist.
func prepareInterruptedMigration(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "work-memory.ltdb")
	stagingPath := dbPath + ".lattice-migrate-0001"
	backupPath := dbPath + ".ladybug-backup-0001"

	staging, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: stagingPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create staging database: %v", err)
	}
	if _, err := staging.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-recovered",
		Kind:    "note",
		Content: "recovered",
		Source:  SourceInput{Kind: "note", ExternalRef: "recovery"},
	}); err != nil {
		t.Fatalf("ingest staging episode: %v", err)
	}
	if err := staging.Close(ctx); err != nil {
		t.Fatalf("close staging engine: %v", err)
	}
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatalf("create backup directory: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhaseBackedUp,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}
	return dbPath
}

func assertRecoveredDatabase(t *testing.T, ctx context.Context, eng Engine, dbPath string) {
	t.Helper()
	episode, err := eng.GetEpisode(ctx, "ep-recovered")
	if err != nil {
		t.Fatalf("expected the staged episode after recovery: %v", err)
	}
	if episode.Content != "recovered" {
		t.Fatalf("unexpected episode content %q", episode.Content)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected the database to be installed at %q: %v", dbPath, err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the migration marker to be cleared, got %v", err)
	}
}

func TestOpenResumesInterruptedMigrationWithCreation(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open after interrupted migration: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	assertRecoveredDatabase(t, ctx, eng, dbPath)
}

func TestOpenResumesInterruptedMigrationWithExplicitDriver(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)

	eng, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("open with explicit driver after interrupted migration: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	assertRecoveredDatabase(t, ctx, eng, dbPath)
}

func TestOpenResumesInterruptedMigrationWithTrailingSeparator(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)

	eng, err := Open(ctx, Config{DatabasePath: dbPath + string(os.PathSeparator), ReadOnly: true})
	if err != nil {
		t.Fatalf("open with a trailing separator after interrupted migration: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	assertRecoveredDatabase(t, ctx, eng, dbPath)
}

func TestOpenResumesInterruptedMigrationWithRelativePath(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)
	t.Chdir(filepath.Dir(dbPath))

	eng, err := Open(ctx, Config{DatabasePath: filepath.Base(dbPath) + string(os.PathSeparator)})
	if err != nil {
		t.Fatalf("open with a relative path after interrupted migration: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	assertRecoveredDatabase(t, ctx, eng, dbPath)
}
