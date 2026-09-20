package yeoul

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMigrateDatabaseBacksUpLegacySiblingFiles(t *testing.T) {
	previousReaderVersion := legacyMigrationReaderVersion
	legacyMigrationReaderVersion = "v0.13.1"
	t.Cleanup(func() { legacyMigrationReaderVersion = previousReaderVersion })

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
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
		ID:      "ep-before-migration",
		Kind:    "note",
		Content: "Ladybug committed state",
		Source:  SourceInput{Kind: "test", ExternalRef: "migration-sibling"},
	}); err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}

	walPath := dbPath + ".wal"
	// The read-only Ladybug engine replays a non-empty .wal file, so the
	// fixture uses an empty but present sidecar to exercise the sibling move.
	walBytes := []byte{}
	if err := os.WriteFile(walPath, walBytes, 0o600); err != nil {
		t.Fatalf("write legacy WAL sidecar: %v", err)
	}

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	if !result.Migrated || result.SourceDriver != string(StorageDriverLadybug) ||
		result.TargetDriver != string(StorageDriverLattice) || result.BackupPath == "" {
		t.Fatalf("unexpected migration result: %#v", result)
	}

	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open migrated lattice database: %v", err)
	}
	episode, err := migrated.GetEpisode(ctx, "ep-before-migration")
	if err != nil {
		t.Fatalf("read migrated episode: %v", err)
	}
	if episode.Content != "Ladybug committed state" {
		t.Fatalf("unexpected migrated episode content %q", episode.Content)
	}
	if err := migrated.Close(ctx); err != nil {
		t.Fatalf("close migrated engine: %v", err)
	}

	backups := migrationBackupNamespaceEntries(t, dbPath)
	wantBackups := []string{
		filepath.Base(result.BackupPath),
		filepath.Base(result.BackupPath) + ".wal",
	}
	if !slices.Equal(backups, wantBackups) {
		t.Fatalf("expected backup set %v, got %v", wantBackups, backups)
	}
	gotWAL, err := os.ReadFile(result.BackupPath + ".wal")
	if err != nil {
		t.Fatalf("read WAL backup: %v", err)
	}
	if string(gotWAL) != string(walBytes) {
		t.Fatalf("WAL backup changed bytes: got %q want %q", gotWAL, walBytes)
	}
	if _, err := os.Stat(walPath); !os.IsNotExist(err) {
		t.Fatalf("original WAL sidecar should be gone, got %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("migration marker should be gone, got %v", err)
	}
}

func TestRecoverDatabaseMigrationRestoresBackedUpSiblingSet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	backupPath := dbPath + ".ladybug-backup-recovery"
	stagingPath := dbPath + ".lattice-migrate-recovery"
	mainBytes := []byte("ladybug-main")
	walBytes := []byte("ladybug-wal")
	if err := os.WriteFile(backupPath, mainBytes, 0o600); err != nil {
		t.Fatalf("write backed-up database: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", walBytes, 0o600); err != nil {
		t.Fatalf("write backed-up WAL sidecar: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhaseBackedUp,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}

	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover migration: %v", err)
	}
	gotMain, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("restored database: %v", err)
	}
	if string(gotMain) != string(mainBytes) {
		t.Fatalf("restored database bytes changed: got %q want %q", gotMain, mainBytes)
	}
	gotWAL, err := os.ReadFile(dbPath + ".wal")
	if err != nil {
		t.Fatalf("restored WAL sidecar: %v", err)
	}
	if string(gotWAL) != string(walBytes) {
		t.Fatalf("restored WAL bytes changed: got %q want %q", gotWAL, walBytes)
	}
	if leftovers := migrationBackupNamespaceEntries(t, dbPath); len(leftovers) != 0 {
		t.Fatalf("backup namespace should be empty after recovery, got %v", leftovers)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("migration marker remains after recovery: %v", err)
	}
}

func migrationBackupNamespaceEntries(t *testing.T, databasePath string) []string {
	t.Helper()
	parent := filepath.Dir(databasePath)
	base := filepath.Base(databasePath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read database parent: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), base+".ladybug-backup-") {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names
}
