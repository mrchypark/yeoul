package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// assertPublicOpenRefusesStaging checks that a public open refuses a staging
// database written under an unsupported application-state version, reports the
// version decision itself, and leaves the migration state untouched for a later
// retry.
func assertPublicOpenRefusesStaging(t *testing.T, dbPath, backupPath, stagingPath string) {
	t.Helper()
	ctx := context.Background()

	eng, err := Open(ctx, Config{DatabasePath: dbPath})
	if eng != nil {
		_ = eng.Close(ctx)
		t.Fatal("expected no engine for an unsupported staging database")
	}
	if err == nil {
		t.Fatal("expected the public open to refuse an unsupported staging database")
	}
	assertUnsupportedStateVersionError(t, err, "2")
	assertStagingMigrationRefused(t, dbPath, backupPath, stagingPath)
}

// TestPublicOpenReportsUnsupportedStagingVersion verifies that the version
// decision survives the recovery that the open runs first. A staged database
// this build cannot read has to be reported as a compatibility refusal, which is
// what tells the caller to upgrade or migrate, rather than as a storage failure
// that hides the reason behind an unreadable database.
func TestPublicOpenReportsUnsupportedStagingVersion(t *testing.T) {
	for _, phase := range []string{migrationPhasePrepared, migrationPhaseBackedUp} {
		t.Run(phase, func(t *testing.T) {
			dbPath, backupPath, stagingPath := prepareUnsupportedStagingMigration(t, phase)
			assertPublicOpenRefusesStaging(t, dbPath, backupPath, stagingPath)
		})
	}
}

// TestPublicOpenAbandonsUnsupportedStagingWhenNothingWasMoved covers the
// prepared marker that still has its original database and no backup, which is
// the crash window before any file was moved. The staged replacement is
// unreadable, but nothing was taken away from the canonical path, so the open
// must abandon the staging rather than install it, and the original database has
// to stay readable and unchanged.
func TestPublicOpenAbandonsUnsupportedStagingWhenNothingWasMoved(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "original-present.ltdb")
	backupPath := dbPath + ".ladybug-backup-0011"
	stagingPath := dbPath + ".lattice-migrate-0011"

	writeLatticeStateFixture(t, stagingPath, "2", false, unknownFieldFixtureNodes())
	writeLatticeStateFixture(t, dbPath, "1", false, nil)
	if err := writeDatabaseMigrationMarker(databaseMigrationMarker{
		Phase:        migrationPhasePrepared,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}
	before := hashDatabaseTree(t, dbPath)

	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("expected the original database to open: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	// The open itself initializes node indexes on the database it accepts, so
	// the comparison below is against the state the open left behind: what
	// matters is that the abandoned staging was never installed over it.
	installed := hashDatabaseTree(t, dbPath)
	if installed == before {
		t.Fatal("expected the open to have initialized the accepted database")
	}
	reopened, err := Open(ctx, Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("expected the accepted database to reopen: %v", err)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
	if after := hashDatabaseTree(t, dbPath); after != installed {
		t.Fatalf("the database changed between two accepted opens: before=%s after=%s", installed, after)
	}
	if _, err := os.Stat(stagingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the abandoned staging database to be removed, got %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the migration marker to be cleared once nothing was moved, got %v", err)
	}
}

// TestPublicOpenStillRecoversSupportedStagingVersion is the control for the
// refusals above: a staged database this build can read is installed and opened
// by the same public open, so the refusal is specific to the version decision
// and not a refusal to recover at all.
func TestPublicOpenStillRecoversSupportedStagingVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := prepareInterruptedMigration(t)

	eng, err := Open(ctx, Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("expected the public open to recover a supported staging database: %v", err)
	}
	assertRecoveredDatabase(t, ctx, eng, dbPath)
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close recovered engine: %v", err)
	}
}
