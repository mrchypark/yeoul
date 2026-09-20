package yeoul

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// prepareUnsupportedStagingMigration reproduces the crash window in which the
// legacy set was moved into the backup namespace and a staging database written
// by an unsupported build was left for recovery to install.
func prepareUnsupportedStagingMigration(t *testing.T, phase string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "unsupported-staging.ltdb")
	backupPath := dbPath + ".ladybug-backup-0009"
	stagingPath := dbPath + ".lattice-migrate-0009"

	writeLatticeStateFixture(t, stagingPath, "2", false, unknownFieldFixtureNodes())
	if err := os.WriteFile(backupPath, []byte("legacy main"), 0o600); err != nil {
		t.Fatalf("write backup main: %v", err)
	}
	if err := os.WriteFile(backupPath+".wal", []byte("legacy wal"), 0o600); err != nil {
		t.Fatalf("write backup wal: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        phase,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}
	return dbPath, backupPath, stagingPath
}

// assertStagingMigrationRefused checks that a refused recovery left every
// member of the migration state in place: the staged database is still at its
// staging path, the legacy set is still in the backup namespace, and the
// canonical path holds no database.
func assertStagingMigrationRefused(t *testing.T, dbPath, backupPath, stagingPath string) {
	t.Helper()
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("expected the staging database to stay at its staging path: %v", err)
	}
	for _, path := range []string{backupPath, backupPath + ".wal"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected backup member %q to stay in place: %v", path, err)
		}
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no database at the canonical path, got %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); err != nil {
		t.Fatalf("expected the migration marker to stay for a later retry: %v", err)
	}
}

func TestRecoverDatabaseMigrationRefusesUnsupportedStagingDatabase(t *testing.T) {
	for _, phase := range []string{migrationPhasePrepared, migrationPhaseBackedUp} {
		t.Run(phase, func(t *testing.T) {
			dbPath, backupPath, stagingPath := prepareUnsupportedStagingMigration(t, phase)

			err := recoverDatabaseMigration(dbPath)
			assertUnsupportedStateVersionError(t, err, "2")
			assertStagingMigrationRefused(t, dbPath, backupPath, stagingPath)
		})
	}
}

func TestRecoverDatabaseMigrationRefusesUnreadableStagingDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "unreadable-staging.ltdb")
	backupPath := dbPath + ".ladybug-backup-0010"
	stagingPath := dbPath + ".lattice-migrate-0010"

	// A staging directory without a database file: the native engine refuses to
	// open it, so installing it would consume the backup for nothing.
	if err := os.MkdirAll(stagingPath, 0o700); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	if err := os.WriteFile(backupPath, []byte("legacy main"), 0o600); err != nil {
		t.Fatalf("write backup main: %v", err)
	}
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhaseBackedUp,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}

	if err := recoverDatabaseMigration(dbPath); err == nil {
		t.Fatal("expected recovery to refuse a staging path that is not a database")
	}
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("expected the staging directory to stay in place: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected the backup to stay in place: %v", err)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no database at the canonical path, got %v", err)
	}
}
