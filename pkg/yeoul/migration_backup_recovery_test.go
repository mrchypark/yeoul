package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverDatabaseMigrationResumesRestoringPhase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "restore.ltdb")
	backupPath := dbPath + ".ladybug-backup-0001"
	stagingPath := dbPath + ".lattice-migrate-0001"

	// The main database was already restored, but its WAL is still in the
	// backup namespace and the marker says a restore is in progress.
	if err := os.WriteFile(dbPath, []byte("restored main"), 0o600); err != nil {
		t.Fatalf("write restored main: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", []byte("wal bytes"), 0o600); err != nil {
		t.Fatalf("write backup wal: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhaseRestoring,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write restoring marker: %v", err)
	}

	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover restoring phase: %v", err)
	}

	data, err := os.ReadFile(dbPath + ".wal")
	if err != nil {
		t.Fatalf("expected the WAL to be restored: %v", err)
	}
	if string(data) != "wal bytes" {
		t.Fatalf("unexpected restored WAL bytes %q", string(data))
	}
	if _, err := os.Stat(backupPath + ".wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no WAL left in the backup namespace, got %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the marker to be cleared, got %v", err)
	}
}

func TestRecoverDatabaseMigrationPreservesInstalledDatabaseOnPreparedRetry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "installed.ltdb")
	backupPath := dbPath + ".ladybug-backup-0002"
	stagingPath := dbPath + ".lattice-migrate-0002"

	eng, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create installed database: %v", err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-installed",
		Kind:    "note",
		Content: "installed",
		Source:  SourceInput{Kind: "test", ExternalRef: "installed"},
	}); err != nil {
		t.Fatalf("ingest installed episode: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close installed engine: %v", err)
	}

	if err := os.WriteFile(backupPath, []byte("legacy main"), 0o600); err != nil {
		t.Fatalf("write backup main: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", []byte("legacy wal"), 0o600); err != nil {
		t.Fatalf("write backup wal: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhasePrepared,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write prepared marker: %v", err)
	}

	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover prepared retry: %v", err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen installed database: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()
	if _, err := reopened.GetEpisode(ctx, "ep-installed"); err != nil {
		t.Fatalf("expected the installed episode to survive: %v", err)
	}

	for _, path := range []string{backupPath, backupPath + ".wal"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected backup member %q to stay in place: %v", path, err)
		}
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the marker to be cleared, got %v", err)
	}
}

func TestMigrateDatabaseKeepsIndependentSiblings(t *testing.T) {
	previousReaderVersion := legacyMigrationReaderVersion
	legacyMigrationReaderVersion = "v0.13.1"
	t.Cleanup(func() { legacyMigrationReaderVersion = previousReaderVersion })

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory")
	legacy, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		legacyLadybugWrites: true,
		CreateIfMissing:     true,
	})
	if err != nil {
		t.Fatalf("open legacy engine: %v", err)
	}
	if _, err := legacy.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-independent",
		Kind:    "note",
		Content: "independent siblings",
		Source:  SourceInput{Kind: "test", ExternalRef: "independent"},
	}); err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}
	if err := os.WriteFile(dbPath+".wal", []byte{}, 0o600); err != nil {
		t.Fatalf("write legacy WAL sidecar: %v", err)
	}

	archivePath := dbPath + ".archive"
	configPath := dbPath + ".config"
	if err := os.WriteFile(archivePath, []byte("independent database"), 0o600); err != nil {
		t.Fatalf("write independent archive: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("settings"), 0o600); err != nil {
		t.Fatalf("write independent config: %v", err)
	}

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}

	for path, want := range map[string]string{archivePath: "independent database", configPath: "settings"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %q to stay in place: %v", path, err)
		}
		if string(data) != want {
			t.Fatalf("expected %q to keep its bytes, got %q", path, string(data))
		}
	}
	if _, err := os.Stat(result.BackupPath + ".archive"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no independent archive in the backup namespace, got %v", err)
	}
	if _, err := os.Stat(result.BackupPath + ".config"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no independent config in the backup namespace, got %v", err)
	}
}

func TestRecoverDatabaseMigrationRestoresPartiallyBackedUpSidecars(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "partial.ltdb")
	backupPath := dbPath + ".ladybug-backup-0004"
	stagingPath := dbPath + ".lattice-migrate-0004"

	if err := os.WriteFile(dbPath, []byte("original main"), 0o600); err != nil {
		t.Fatalf("write original main: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", []byte("committed wal"), 0o600); err != nil {
		t.Fatalf("write backed-up wal: %v", err)
	}
	if err := os.MkdirAll(stagingPath, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhasePrepared,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write prepared marker: %v", err)
	}

	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover partial backup: %v", err)
	}

	data, err := os.ReadFile(dbPath + ".wal")
	if err != nil {
		t.Fatalf("expected the WAL to be restored: %v", err)
	}
	if string(data) != "committed wal" {
		t.Fatalf("unexpected restored WAL bytes %q", string(data))
	}
	if _, err := os.Stat(backupPath + ".wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no WAL left in the backup namespace, got %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the marker to be cleared, got %v", err)
	}
}

func TestRestoreLegacyDatabaseSetStopsWhenMarkerWriteFails(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "blocked.ltdb")
	backupPath := dbPath + ".ladybug-backup-0005"
	stagingPath := dbPath + ".lattice-migrate-0005"

	if err := os.WriteFile(dbPath, []byte("original main"), 0o600); err != nil {
		t.Fatalf("write original main: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", []byte("committed wal"), 0o600); err != nil {
		t.Fatalf("write backed-up wal: %v", err)
	}

	// Block the marker path with a non-empty directory so the atomic marker
	// rename must fail.
	markerPath := databaseMigrationMarkerPath(dbPath)
	if err := os.MkdirAll(filepath.Join(markerPath, "block"), 0o700); err != nil {
		t.Fatalf("block marker path: %v", err)
	}

	marker := databaseMigrationMarker{
		Phase:        migrationPhaseRestoring,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}
	if err := restoreLegacyDatabaseSet(marker, markerPath); err == nil {
		t.Fatal("expected the marker write failure to surface")
	}
	if _, err := os.Stat(backupPath + ".wal"); err != nil {
		t.Fatalf("expected the backup member to stay in place when the marker cannot be written: %v", err)
	}
	if _, err := os.Stat(dbPath + ".wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no restoration attempt before the marker write succeeds, got %v", err)
	}
}
