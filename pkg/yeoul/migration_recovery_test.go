package yeoul

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestReadOnlyOpenRecoversInterruptedMigrationWithoutOptIn pins the boundary of
// the read-only no-mutation contract. AllowMigration governs whether a
// read-only open may start a legacy conversion; it does not govern finishing a
// migration that already started. A marker left by a crash is proof the
// conversion was authorized and the only complete snapshot may still be in
// staging, so a read-only open completes the recovery and then serves reads.
// Without this, a reader would be locked out of a database it never asked to
// convert.
func TestReadOnlyOpenRecoversInterruptedMigrationWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)

	eng, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only open after interrupted migration: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	assertRecoveredDatabase(t, ctx, eng, dbPath)

	// The recovery finished the interrupted protocol; it did not grant the
	// reader a licence to convert anything else. The write path stays closed.
	_, writeErr := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "must not be written",
		Source:  SourceInput{Kind: "note"},
	})
	if writeErr == nil {
		t.Fatal("expected the recovered read-only engine to refuse writes")
	}
	var writeYeoulErr *Error
	if !errors.As(writeErr, &writeYeoulErr) {
		t.Fatalf("expected a structured Yeoul error, got %T", writeErr)
	}
	if writeYeoulErr.Code != ErrNotSupported {
		t.Fatalf("expected %s, got %s", ErrNotSupported, writeYeoulErr.Code)
	}
}

// TestReadOnlyOpenWithoutMarkerLeavesDatabaseUntouched is the other half of the
// same contract: with no pending marker there is nothing to recover, so a
// read-only open is a pure read. It must not convert, back up, stage, or
// otherwise rewrite a database the caller only asked to inspect.
func TestReadOnlyOpenWithoutMarkerLeavesDatabaseUntouched(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "read-only-no-marker.ltdb")

	writer, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	episode, err := writer.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-read-only",
		Kind:    "note",
		Content: "read-only fixture",
		Source:  SourceInput{Kind: "note", ExternalRef: "read-only"},
	})
	if err != nil {
		t.Fatalf("ingest fixture episode: %v", err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	before := databaseTreeSnapshot(t, dbPath)
	if len(before) == 0 {
		t.Fatal("expected the fixture to write at least one data file")
	}

	reader, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only open: %v", err)
	}
	got, err := reader.GetEpisode(ctx, episode.EpisodeID)
	if err != nil {
		t.Fatalf("read fixture episode: %v", err)
	}
	if got.Content != "read-only fixture" {
		t.Fatalf("unexpected episode content %q", got.Content)
	}
	if err := reader.Close(ctx); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	if after := databaseTreeSnapshot(t, dbPath); !equalStringMaps(before, after) {
		t.Fatalf("read-only open changed the database file set\nbefore: %v\nafter:  %v", before, after)
	}
	assertNoMigrationArtifacts(t, dbPath)
}

// databaseTreeSnapshot hashes every data file that belongs to one database,
// whether the database is a single file or a directory (the LatticeDB layout).
// Ownership lock files are excluded: taking a lock creates them regardless of
// whether the read succeeds, so they are not evidence of a mutation. Migration
// marker files are included on purpose, so a test can prove none was written.
func databaseTreeSnapshot(t *testing.T, dbPath string) map[string]string {
	t.Helper()
	if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
		sums := make(map[string]string)
		err := filepath.WalkDir(dbPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(entry.Name(), databaseOwnershipSuffix) {
				return nil
			}
			rel, relErr := filepath.Rel(dbPath, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sums[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot database directory %s: %v", dbPath, err)
		}
		return sums
	}
	return legacyDatabaseFileSet(t, dbPath)
}
