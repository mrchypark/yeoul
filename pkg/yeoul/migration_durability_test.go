package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// The success path has eight namespace transitions, each of which must sync
	// the database's parent directory: the prepared marker publication, the
	// sidecar backup renames, the main database backup rename, the backed-up
	// marker publication, the staging install, the installed marker
	// publication, the marker removal, and the staging cleanup.
	if len(*calls) != 8 {
		t.Fatalf("expected 8 directory syncs on the migration success path, got %d", len(*calls))
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

// TestMigrateDatabaseFlushesSidecarsBeforeMainBackup pins the backup ordering
// invariant: the sidecar renames must be durable before the main database
// moves, because recovery treats a main database in the backup namespace as
// proof that the complete set moved with it.
func TestMigrateDatabaseFlushesSidecarsBeforeMainBackup(t *testing.T) {
	enableInProcessLegacyMigration(t)

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.lbug")
	writeLegacyDatabaseFixture(t, dbPath)

	var events []string
	previousSync := syncMigrationDirectory
	syncMigrationDirectory = func(string) error {
		events = append(events, "sync")
		return nil
	}
	t.Cleanup(func() { syncMigrationDirectory = previousSync })

	previousRename := migrationRename
	migrationRename = func(source, target string) error {
		events = append(events, "rename:"+filepath.Base(source))
		return previousRename(source, target)
	}
	t.Cleanup(func() { migrationRename = previousRename })

	if _, err := MigrateDatabase(ctx, dbPath); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}

	sidecarRename := -1
	mainRename := -1
	syncAfterSidecar := -1
	for index, event := range events {
		switch event {
		case "rename:" + filepath.Base(dbPath) + ".wal":
			sidecarRename = index
		case "rename:" + filepath.Base(dbPath):
			mainRename = index
		case "sync":
			if syncAfterSidecar == -1 && sidecarRename != -1 && mainRename == -1 {
				syncAfterSidecar = index
			}
		}
	}
	if sidecarRename == -1 || mainRename == -1 {
		t.Fatalf("expected both the sidecar and main renames, got %v", events)
	}
	if sidecarRename > mainRename {
		t.Fatalf("expected the sidecar to move before the main database, got %v", events)
	}
	if syncAfterSidecar == -1 {
		t.Fatalf("expected a directory flush between the sidecar and main renames, got %v", events)
	}
}

// blockMigrationRenames replaces the migration rename hook and restores the
// previous hook with t.Cleanup, returning a function that restores it early. A
// nil error from the hook performs the real rename.
func blockMigrationRenames(t *testing.T, hook func(source, target string) error) func() {
	t.Helper()
	previous := migrationRename
	restore := func() { migrationRename = previous }
	migrationRename = func(source, target string) error {
		if err := hook(source, target); err != nil {
			return err
		}
		return previous(source, target)
	}
	t.Cleanup(restore)
	return restore
}

// TestMigrateDatabaseKeepsStagingWhenRollbackFails reproduces the ST-05 window:
// the staging install fails and the legacy rollback fails as well. Both
// failures must be reported, and the verified staging database must survive so
// recovery can install it without manual filesystem edits.
func TestMigrateDatabaseKeepsStagingWhenRollbackFails(t *testing.T) {
	enableInProcessLegacyMigration(t)

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "both-failures.lbug")
	writeLegacyDatabaseFixture(t, dbPath)

	installFailure := errors.New("injected install failure")
	rollbackFailure := errors.New("injected rollback failure")
	restoreRenames := blockMigrationRenames(t, func(source, target string) error {
		// Restoring a backup member is the rollback; installing the staging
		// database is the cutover. Both fail here, which is the ST-05 window.
		if strings.HasPrefix(filepath.Base(source), filepath.Base(dbPath)+".ladybug-backup-") {
			return rollbackFailure
		}
		if filepath.Clean(target) == filepath.Clean(dbPath) {
			return installFailure
		}
		return nil
	})

	_, err := MigrateDatabase(ctx, dbPath)
	if err == nil {
		t.Fatal("expected the migration to fail")
	}
	if !strings.Contains(err.Error(), installFailure.Error()) {
		t.Fatalf("expected the install failure to be reported, got %v", err)
	}
	if !strings.Contains(err.Error(), rollbackFailure.Error()) {
		t.Fatalf("expected the rollback failure to be reported, got %v", err)
	}

	// The staging database is the only verified copy of the converted data
	// once the rollback fails, so it must survive the failed migration.
	staging, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	var stagingNames []string
	for _, entry := range staging {
		if strings.HasPrefix(entry.Name(), filepath.Base(dbPath)+".lattice-migrate-") {
			stagingNames = append(stagingNames, entry.Name())
		}
	}
	if len(stagingNames) == 0 {
		t.Fatal("expected the verified staging database to survive the failed rollback")
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); err != nil {
		t.Fatalf("expected the migration marker to survive for recovery: %v", err)
	}

	// Recovery must complete the interrupted migration from the surviving
	// artifacts alone: the marker still points at the staging database and the
	// legacy backup is still in place.
	restoreRenames()
	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover after the failed rollback: %v", err)
	}
	engine, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open the recovered database: %v", err)
	}
	defer func() { _ = engine.Close(ctx) }()
	if _, err := engine.GetEpisode(ctx, "ep-durability"); err != nil {
		t.Fatalf("expected the recovered episode: %v", err)
	}
}
