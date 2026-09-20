package yeoul

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestMigrationOwnershipBlocksConcurrentOwners verifies that a migration holds
// the exclusive ownership lock for its whole duration: opens and further
// migrations are refused while it is held and succeed once it is released.
func TestMigrationOwnershipBlocksConcurrentOwners(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "owned.lbug")
	createOwnedLegacyDatabase(t, dbPath)

	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("acquire exclusive ownership: %v", err)
	}

	if _, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true}); err == nil {
		t.Fatal("expected an open to be refused while a migration owns the database")
	} else if !strings.Contains(err.Error(), "database migration is in progress") {
		t.Fatalf("expected a migration-in-progress refusal, got %v", err)
	}
	if _, err := MigrateDatabase(ctx, dbPath); err == nil {
		t.Fatal("expected a second migration to be refused while the lock is held")
	} else if !strings.Contains(err.Error(), "another database migration is in progress") {
		t.Fatalf("expected a concurrent-migration refusal, got %v", err)
	}

	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
	opened, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("expected the database to open after the lock was released: %v", err)
	}
	if err := opened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
}

// TestOpenRefusesDuringMigrationWithoutCreatingADatabase covers the open that
// used to pass a stat-based check, pause, and then create an empty database at
// the path a migration had just vacated.
func TestOpenRefusesDuringMigrationWithoutCreatingADatabase(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "absent.ltdb")

	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("acquire exclusive ownership: %v", err)
	}
	defer func() { _ = exclusive.Release() }()

	// Both spellings must be refused: the path is normalized before the
	// ownership lookup, so a trailing separator cannot bypass it.
	for _, candidate := range []string{dbPath, dbPath + string(os.PathSeparator)} {
		if _, err := Open(ctx, Config{DatabasePath: candidate, CreateIfMissing: true}); err == nil {
			t.Fatalf("expected open of %q to be refused during a migration", candidate)
		} else if !strings.Contains(err.Error(), "database migration is in progress") {
			t.Fatalf("expected a migration-in-progress refusal for %q, got %v", candidate, err)
		}
	}

	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused open must not create a database at %q, got %v", dbPath, err)
	}
}

// TestOpenHoldsOwnershipForTheStoreLifetime verifies that an open store keeps
// the shared lock, so a migration can neither snapshot a database that store
// will keep writing nor install a replacement underneath it.
func TestOpenHoldsOwnershipForTheStoreLifetime(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "held.ltdb")

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := acquireDatabaseOwnership(dbPath, true); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected exclusive ownership to be refused while a store is open, got %v", err)
	}
	if _, err := MigrateDatabase(ctx, dbPath); err == nil {
		t.Fatal("expected a migration to be refused while a store is open")
	}

	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("expected ownership to be free after the store closed: %v", err)
	}
	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
}

// TestConcurrentReadersShareOwnership guards against over-locking: readers take
// TestOpenRefusesRecoveryWithoutOwnership verifies that an open never runs
// migration recovery without ownership. A pending marker is visible to this
// process only while another owner holds the lock, and recovery moves files, so
// the open has to fail instead of installing a recovery over a live database.
func TestOpenRefusesRecoveryWithoutOwnership(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pending.ltdb")

	marker := databaseMigrationMarker{
		Phase:        migrationPhaseBackedUp,
		DatabasePath: dbPath,
		BackupPath:   dbPath + ".ladybug-backup-20240101T000000.000000000Z",
		StagingPath:  dbPath + ".lattice-migrate-20240101T000000.000000000Z",
	}
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		t.Fatalf("write migration marker: %v", err)
	}

	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("acquire exclusive ownership: %v", err)
	}
	defer func() { _ = exclusive.Release() }()

	for _, readOnly := range []bool{true, false} {
		if _, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: readOnly}); err == nil {
			t.Fatalf("expected an open with read_only=%v to refuse recovery without ownership", readOnly)
		} else if !strings.Contains(err.Error(), "database migration is in progress") {
			t.Fatalf("expected a migration-in-progress refusal for read_only=%v, got %v", readOnly, err)
		}
	}

	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); err != nil {
		t.Fatalf("a refused recovery must leave the marker in place: %v", err)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused recovery must not create a database at %q, got %v", dbPath, err)
	}
}

// TestCreateIfMissingCreatesTheParentDirectory verifies that an explicit
// creation still works for a database whose parent directory does not exist.
// Ownership is acquired through a sibling file, so the directory has to exist
// before the lock is taken, while the database itself is still created only
// after the lock is held.
func TestCreateIfMissingCreatesTheParentDirectory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "nested", "deeper", "memory.ltdb")

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database in a missing directory: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected the database to be created at %q: %v", dbPath, err)
	}
	if _, err := os.Stat(databaseOwnershipPath(dbPath)); err != nil {
		t.Fatalf("expected the ownership file to live next to the database: %v", err)
	}
}

// TestConcurrentReadersShareOwnership guards against over-locking: readers take
// the shared ownership lock, so they must not exclude each other. A writer
// still takes the canonical engine's exclusive path lock, which is separate.
func TestConcurrentReadersShareOwnership(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.ltdb")

	created, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	if err := created.Close(ctx); err != nil {
		t.Fatalf("close created database: %v", err)
	}

	first, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open first reader: %v", err)
	}
	second, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		_ = first.Close(ctx)
		t.Fatalf("expected a second reader to share ownership: %v", err)
	}
	if err := errors.Join(second.Close(ctx), first.Close(ctx)); err != nil {
		t.Fatalf("close readers: %v", err)
	}
}

// TestLeftoverOwnershipFileDoesNotBlockOpens verifies that the ownership file
// is a marker for the kernel lock rather than the lock itself: a file left
// behind by a crashed owner holds no lock, and no caller removes it on another
// owner's behalf.
func TestLeftoverOwnershipFileDoesNotBlockOpens(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "leftover.ltdb")
	ownershipPath := databaseOwnershipPath(dbPath)
	if err := os.WriteFile(ownershipPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write leftover ownership file: %v", err)
	}

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("expected the leftover ownership file not to block an open: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	if _, err := os.Stat(ownershipPath); err != nil {
		t.Fatalf("ownership file must not be removed on another owner's behalf: %v", err)
	}
}

const (
	ownershipHelperEnv      = "YEOUL_TEST_OWNERSHIP_HELPER"
	ownershipHelperDBEnv    = "YEOUL_TEST_OWNERSHIP_DATABASE"
	ownershipHelperReadyEnv = "YEOUL_TEST_OWNERSHIP_READY"
)

// TestDatabaseOwnershipHelperProcess is not a test: it is the child process
// TestKilledOwnerReleasesOwnership starts and kills while it owns a database.
func TestDatabaseOwnershipHelperProcess(t *testing.T) {
	if os.Getenv(ownershipHelperEnv) != "1" {
		t.Skip("helper process for TestKilledOwnerReleasesOwnership")
	}
	if _, err := acquireDatabaseOwnership(os.Getenv(ownershipHelperDBEnv), true); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Getenv(ownershipHelperReadyEnv), []byte("ready"), 0o600); err != nil {
		os.Exit(4)
	}
	select {}
}

// TestKilledOwnerReleasesOwnership verifies the property that makes the lock
// crash-safe on every supported platform: the kernel drops the lock when the
// owning process dies, so a killed migration never leaves a database that later
// opens must reclaim by hand.
func TestKilledOwnerReleasesOwnership(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "killed.lbug")
	readyPath := filepath.Join(dir, "ready")

	command := exec.Command(os.Args[0], "-test.run=TestDatabaseOwnershipHelperProcess")
	command.Env = append(os.Environ(),
		ownershipHelperEnv+"=1",
		ownershipHelperDBEnv+"="+dbPath,
		ownershipHelperReadyEnv+"="+readyPath,
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start ownership helper: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	waitForOwnershipHelper(t, readyPath)

	if _, err := acquireDatabaseOwnership(dbPath, false); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected a live owner to refuse a shared lock, got %v", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill ownership helper: %v", err)
	}
	if _, err := command.Process.Wait(); err != nil {
		t.Fatalf("wait for ownership helper: %v (%s)", err, stderr.String())
	}

	ownership, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("expected the killed owner's lock to be released: %v", err)
	}
	if err := ownership.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
	if _, err := os.Stat(databaseOwnershipPath(dbPath)); err != nil {
		t.Fatalf("expected the ownership file to survive the crash: %v", err)
	}
}

func waitForOwnershipHelper(t *testing.T, readyPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("ownership helper did not report readiness")
}
