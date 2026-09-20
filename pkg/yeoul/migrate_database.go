package yeoul

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const (
	migrationPhasePrepared  = "prepared"
	migrationPhaseBackedUp  = "backed_up"
	migrationPhaseInstalled = "installed"
	migrationPhaseRestoring = "restoring"

	legacyMigrationHelperEnv = "YEOUL_LEGACY_MIGRATION_HELPER"

	// migrationOwnershipAttempts and migrationOwnershipRetryDelay bound how long
	// a migration waits for a contended ownership lock. The wait absorbs the
	// handoff window where this process has released the lock and the pinned
	// helper has not acquired it yet, plus a short-lived reader. A real
	// migration or a long-lived store holds the lock for far longer than this.
	migrationOwnershipAttempts   = 8
	migrationOwnershipRetryDelay = 25 * time.Millisecond
)

// legacyMigrationSidecarSuffixes lists the native Ladybug sidecars that belong
// to one database file. Arbitrary dotted siblings (for example
// "memory.archive") are independent data and are never moved.
var legacyMigrationSidecarSuffixes = []string{".wal", ".shadow", ".tmp"}

// Set only on the version-pinned migration helper with go build -ldflags -X.
var legacyMigrationReaderVersion string

// DatabaseMigrationResult describes one legacy Ladybug to LatticeDB migration.
type DatabaseMigrationResult struct {
	DatabasePath string `json:"database_path"`
	BackupPath   string `json:"backup_path"`
	SourceDriver string `json:"source_driver"`
	TargetDriver string `json:"target_driver"`
	Migrated     bool   `json:"migrated"`
}

type databaseMigrationMarker struct {
	Phase        string `json:"phase"`
	DatabasePath string `json:"database_path"`
	BackupPath   string `json:"backup_path"`
	StagingPath  string `json:"staging_path"`
}

// MigrateDatabase converts a legacy Ladybug database in place and retains the
// original database as a timestamped sibling backup.
func MigrateDatabase(ctx context.Context, databasePath string) (*DatabaseMigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if databasePath == "" {
		return nil, errorf(ErrConfigInvalid, "database path is required for migration", nil, nil)
	}
	databasePath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve migration database path: %w", err)
	}
	// The ownership lock, the migration marker, and the native engine's own
	// lock all have to name the same database. A path reached through a
	// symlinked directory is one database to the engine but a second ownership
	// namespace to Yeoul, so the aliases are resolved before any of them is
	// used.
	databasePath, err = resolveDatabasePathAliases(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve migration database path: %w", err)
	}
	// Ownership spans loading the source, building and verifying the staging
	// database, the marker phases, and the cutover, so no writer can commit
	// records the snapshot misses and no competing migrator can move the file
	// set or overwrite the shared marker.
	ownership, err := acquireMigrationOwnership(databasePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = ownership.Release()
	}()

	if err := recoverDatabaseMigration(databasePath); err != nil {
		return nil, err
	}

	lattice, latticeErr := newLatticeStore(Config{DatabasePath: databasePath, ReadOnly: true})
	if latticeErr != nil && errors.Is(latticeErr, errUnsupportedStateVersion) {
		// The path holds a LatticeDB state written under an application-state
		// version this build cannot read. The legacy conversion below would replace
		// the database, so the version rejection is reported as it stands.
		return nil, latticeErr
	}
	if latticeErr == nil {
		_, loadErr := lattice.Load()
		closeErr := lattice.Close()
		if loadErr == nil {
			if closeErr != nil {
				return nil, closeErr
			}
			return &DatabaseMigrationResult{
				DatabasePath: databasePath,
				SourceDriver: string(StorageDriverLattice),
				TargetDriver: string(StorageDriverLattice),
			}, nil
		}
	}
	if legacyMigrationReaderVersion != "v0.13.1" {
		return migrateDatabaseWithLegacyHelper(ctx, databasePath, ownership)
	}

	return migrateLegacyDatabaseInProcess(databasePath)
}

// migrateDatabaseWithLegacyHelper converts the database with the version-pinned
// helper binary. The helper is a separate process that has to own the database
// itself, so this process hands ownership over instead of delegating it: it
// releases the exclusive lock before spawning, and the helper acquires the same
// operating-system lock on startup and refuses to touch the database when it
// cannot.
//
// Delegating the lock by flag alone is not enough. A parent that is killed
// while the helper runs would drop the lock and let a competing migration or
// writer proceed while the orphaned helper still held a stale snapshot, so the
// helper re-acquires ownership and re-verifies the on-disk state under that
// lock before it changes anything.
func migrateDatabaseWithLegacyHelper(ctx context.Context, databasePath string, ownership *databaseOwnershipLock) (*DatabaseMigrationResult, error) {
	helperPath, err := legacyMigrationHelperPath()
	if err != nil {
		return nil, errorf(ErrStorageFailed, "locate version-pinned legacy migration helper", map[string]any{
			"database_path": databasePath,
		}, err)
	}
	if _, err := os.Stat(helperPath); err != nil {
		return nil, errorf(ErrStorageFailed, "version-pinned legacy migration helper is unavailable", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
		}, err)
	}

	// Release before spawning: the helper owns the database for the whole
	// conversion, so both processes must never hold the same lock at once. The
	// deferred release in MigrateDatabase is a no-op after this.
	if err := ownership.Release(); err != nil {
		return nil, errorf(ErrStorageFailed, "release database ownership for the legacy migration helper", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
		}, err)
	}

	command := exec.CommandContext(ctx, helperPath, "admin", "migrate-db", "--db", databasePath, "--json")
	command.Env = legacyMigrationHelperEnvironment(os.Environ())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, errorf(ErrStorageFailed, "version-pinned legacy migration helper failed", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
			"stderr":        strings.TrimSpace(stderr.String()),
		}, err)
	}

	var result DatabaseMigrationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, errorf(ErrStorageFailed, "decode version-pinned legacy migration result", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
		}, err)
	}
	if filepath.Clean(result.DatabasePath) != filepath.Clean(databasePath) ||
		result.TargetDriver != string(StorageDriverLattice) {
		return nil, errorf(ErrStorageFailed, "version-pinned legacy migration helper returned an invalid result", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
		}, nil)
	}
	if !result.Migrated {
		// The helper owns the database itself and re-checks it under that lock,
		// so a competing migration that finished during the handoff makes the
		// helper report the database as already converted. That is a valid
		// outcome, not a broken helper.
		if result.SourceDriver != string(StorageDriverLattice) {
			return nil, errorf(ErrStorageFailed, "version-pinned legacy migration helper returned an invalid result", map[string]any{
				"database_path": databasePath,
				"helper_path":   helperPath,
			}, nil)
		}
		return &result, nil
	}
	if result.SourceDriver != string(StorageDriverLadybug) || result.BackupPath == "" {
		return nil, errorf(ErrStorageFailed, "version-pinned legacy migration helper returned an invalid result", map[string]any{
			"database_path": databasePath,
			"helper_path":   helperPath,
		}, nil)
	}
	return &result, nil
}

func legacyMigrationHelperEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "LD_LIBRARY_PATH", "LD_PRELOAD", "DYLD_LIBRARY_PATH", "DYLD_FALLBACK_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES":
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func legacyMigrationHelperPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(legacyMigrationHelperEnv)); configured != "" {
		return filepath.Abs(configured)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	helperName := "yeoul-migrate-v0131"
	if runtime.GOOS == "windows" {
		helperName += ".exe"
	}
	return filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "libexec", "ladybug-v0131", helperName)), nil
}

func migrateLegacyDatabaseInProcess(databasePath string) (*DatabaseMigrationResult, error) {
	// The caller already holds the exclusive ownership lock for the whole
	// migration, so a concurrent writer or migrator cannot change the source
	// after this snapshot or replace the shared marker.
	markerPath := databaseMigrationMarkerPath(databasePath)

	legacy, err := newLadybugStore(Config{Driver: StorageDriverLadybug, DatabasePath: databasePath, ReadOnly: true})
	if err != nil {
		return nil, errorf(ErrStorageFailed, "open legacy ladybug database for migration", map[string]any{
			"database_path": databasePath,
		}, err)
	}
	state, err := legacy.Load()
	closeErr := legacy.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	stagingPath := databasePath + ".lattice-migrate-" + timestamp
	backupPath := databasePath + ".ladybug-backup-" + timestamp
	marker := databaseMigrationMarker{
		Phase:        migrationPhasePrepared,
		DatabasePath: databasePath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}
	// The staging database is the only verified copy of the converted data
	// until the legacy set is back in place, so it is never deleted here: a
	// cleanup that runs after a failed rollback would destroy the input
	// recovery needs. Every removal below happens only once the protocol no
	// longer depends on it.
	defer func() {
		// A staging database is a recovery input only once a marker references
		// it; before that point it is unreferenced, so a failure must not leak a
		// database-sized copy. Both helpers read the on-disk marker, so a
		// publication that failed after its rename still leaves the pair intact.
		discardUnpublishedMigrationStaging(markerPath, stagingPath)
		cleanupAbandonedMigrationStaging(marker, markerPath)
	}()

	target, err := newLatticeStore(Config{Driver: StorageDriverLattice, DatabasePath: stagingPath, CreateIfMissing: true})
	if err != nil {
		return nil, err
	}
	if err := target.Save(*state); err != nil {
		_ = target.Close()
		return nil, err
	}
	if err := target.Close(); err != nil {
		return nil, err
	}

	verification, err := newLatticeStore(Config{Driver: StorageDriverLattice, DatabasePath: stagingPath, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	verifiedState, loadErr := verification.Load()
	closeErr = verification.Close()
	if loadErr != nil {
		return nil, loadErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	equal, err := persistedStatesEqual(*state, *verifiedState)
	if err != nil {
		return nil, err
	}
	if !equal {
		return nil, errorf(ErrStorageFailed, "lattice migration verification failed", map[string]any{
			"database_path": databasePath,
		}, nil)
	}

	if err := writeDatabaseMigrationMarker(marker); err != nil {
		return nil, err
	}
	if err := moveLegacyDatabaseFileSet(databasePath, backupPath); err != nil {
		// The legacy set may be partially moved even though the caller sees a
		// failure: a sidecar can sit in the backup namespace while the main
		// database never left. Restore the complete set before the deferred
		// cleanup is allowed to discard the marker and the staging copy, and
		// keep both when that restoration is incomplete or uncertain.
		return nil, errors.Join(err, restoreLegacyDatabaseSet(marker, markerPath))
	}
	marker.Phase = migrationPhaseBackedUp
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		if restoreErr := restoreLegacyDatabaseSet(marker, markerPath); restoreErr != nil {
			return nil, errors.Join(err, fmt.Errorf("restore legacy database backup: %w", restoreErr))
		}
		return nil, err
	}
	if published, err := installStagingDatabase(marker, markerPath); err != nil {
		// Once the rename is durable the converted database is the live
		// database; recovery finishes that installation instead of rolling it
		// back. Before that point the legacy set is still the source of truth
		// and is restored, and the staging database survives a failed restore
		// because it is the only verified copy of the converted data.
		if published {
			return nil, err
		}
		rollbackErr := restoreLegacyDatabaseSet(marker, markerPath)
		if rollbackErr != nil {
			return nil, errors.Join(err, fmt.Errorf("restore legacy database backup: %w", rollbackErr))
		}
		return nil, err
	}
	marker.Phase = migrationPhaseInstalled
	return &DatabaseMigrationResult{
		DatabasePath: databasePath,
		BackupPath:   backupPath,
		SourceDriver: string(StorageDriverLadybug),
		TargetDriver: string(StorageDriverLattice),
		Migrated:     true,
	}, nil
}

type migrationFileMove struct {
	source string
	target string
}

// migrationRename is the rename hook for every protocol namespace move. Tests
// replace it to inject move failures; production always runs os.Rename.
var migrationRename = os.Rename

// installStagingDatabase publishes the verified staging database as the live
// database and finalizes the protocol: the installation intent is durable
// before the rename, the rename is durable before the installation is
// recorded, and the marker is cleared only after that record is durable. The
// returned flag reports whether the rename completed, which is the point where
// the converted database becomes the live database and a rollback would
// destroy the only usable copy. A failure leaves the marker in place so
// recovery resumes the installation instead of discarding the converted data.
func installStagingDatabase(marker databaseMigrationMarker, markerPath string) (bool, error) {
	marker.Phase = migrationPhaseBackedUp
	if !migrationMarkerRecordsPhase(markerPath, migrationPhaseBackedUp) {
		if err := writeDatabaseMigrationMarker(marker); err != nil {
			return false, err
		}
	}
	if err := migrationRename(marker.StagingPath, marker.DatabasePath); err != nil {
		return false, fmt.Errorf("install lattice database: %w", err)
	}
	if err := syncMigrationDirectory(filepath.Dir(marker.DatabasePath)); err != nil {
		return true, fmt.Errorf("sync installed lattice database directory: %w", err)
	}
	marker.Phase = migrationPhaseInstalled
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		return true, err
	}
	return true, clearDatabaseMigrationState(marker, markerPath)
}

// clearDatabaseMigrationState removes the migration marker after discarding
// the staging database. The marker is removed before the staging cleanup is
// attempted, so a crash can never leave a marker that points at deleted
// staging data; a staging copy left behind by an interrupted cleanup is an
// orphan with no marker, which is inert.
func clearDatabaseMigrationState(marker databaseMigrationMarker, markerPath string) error {
	if err := removeDatabaseMigrationMarker(markerPath); err != nil {
		return err
	}
	if marker.StagingPath != "" {
		// Best effort: an orphan staging copy left by an interrupted cleanup is
		// inert because its marker is already gone, so it must not fail an
		// otherwise complete migration.
		if err := os.RemoveAll(marker.StagingPath); err != nil {
			return nil
		}
		_ = syncMigrationDirectory(filepath.Dir(marker.DatabasePath))
	}
	return nil
}

// discardUnpublishedMigrationStaging removes a staging database that no
// published marker references. A marker is what turns the staging copy into a
// recovery input, so before publication a failed migration would otherwise
// leak a database-sized copy on every attempt. The on-disk marker is consulted
// rather than the caller's in-memory phase, because a publication that failed
// after its rename still left the pair on disk.
func discardUnpublishedMigrationStaging(markerPath, stagingPath string) {
	if stagingPath == "" {
		return
	}
	if _, err := os.Stat(markerPath); err == nil {
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	if _, err := os.Stat(stagingPath); err != nil {
		return
	}
	if err := os.RemoveAll(stagingPath); err != nil {
		return
	}
	_ = syncMigrationDirectory(filepath.Dir(markerPath))
}

// cleanupAbandonedMigrationStaging removes the staging database only when the
// protocol has no further use for it: the on-disk marker must still record the
// prepared phase (so no installation was published) and the legacy set must be
// back in place. The marker is removed before the staging cleanup, so a crash
// never leaves a marker that points at deleted staging data.
func cleanupAbandonedMigrationStaging(marker databaseMigrationMarker, markerPath string) {
	if marker.StagingPath == "" || marker.Phase != migrationPhasePrepared {
		return
	}
	if !migrationMarkerRecordsPhase(markerPath, migrationPhasePrepared) {
		return
	}
	// The staging copy and the marker are recovery inputs whenever any member
	// of the legacy set is still stranded in the backup namespace, so cleanup
	// only runs once the complete set is verifiably back at the live path.
	restored, err := legacyDatabaseSetFullyRestored(marker)
	if err != nil || !restored {
		return
	}
	// The observed file set describes where the members are, not that those
	// locations survive a power loss. A restoration whose rename was never
	// flushed looks identical here, so flush the namespace before the marker is
	// unlinked: otherwise a power loss could preserve the marker removal while
	// reverting the restoration it depends on.
	if err := syncMigrationDirectory(filepath.Dir(markerPath)); err != nil {
		return
	}
	if err := removeDatabaseMigrationMarker(markerPath); err != nil {
		return
	}
	if err := os.RemoveAll(marker.StagingPath); err != nil {
		return
	}
	_ = syncMigrationDirectory(filepath.Dir(marker.DatabasePath))
}

// legacyDatabaseSetFullyRestored reports whether every member of the legacy
// database set is present at its original path and no member is left in the
// backup namespace. A stranded member means the legacy database is incomplete,
// so the migration state must be preserved for recovery instead of discarded.
func legacyDatabaseSetFullyRestored(marker databaseMigrationMarker) (bool, error) {
	if _, err := os.Stat(marker.DatabasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if _, err := os.Stat(marker.BackupPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	parent := filepath.Dir(marker.DatabasePath)
	backupBase := filepath.Base(marker.BackupPath)
	for _, suffix := range legacyMigrationSidecarSuffixes {
		if _, err := os.Stat(filepath.Join(parent, backupBase+suffix)); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

// migrationMarkerRecordsPhase reports whether the on-disk marker records the
// given protocol phase. Cleanup decisions read the persisted phase rather than
// the caller's in-memory copy, because only the persisted phase describes what
// a crash left behind.
func migrationMarkerRecordsPhase(markerPath, phase string) bool {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	var marker databaseMigrationMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false
	}
	return marker.Phase == phase
}

// legacyMigrationFileMoves lists the native Ladybug file set that must move
// with the database: the main path plus the engine's sidecar files. The
// migration marker, staging database, backup namespace, and unrelated dotted
// siblings are never part of the set.
func legacyMigrationFileMoves(databasePath, backupPath string) ([]migrationFileMove, error) {
	parent := filepath.Dir(databasePath)
	backupBase := filepath.Base(backupPath)
	sidecars := make([]migrationFileMove, 0, len(legacyMigrationSidecarSuffixes))
	for _, suffix := range legacyMigrationSidecarSuffixes {
		source := databasePath + suffix
		if _, err := os.Stat(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat legacy database sidecar %q: %w", source, err)
		}
		sidecars = append(sidecars, migrationFileMove{
			source: source,
			target: filepath.Join(parent, backupBase+suffix),
		})
	}
	moves := make([]migrationFileMove, 0, len(sidecars)+1)
	moves = append(moves, sidecars...)
	moves = append(moves, migrationFileMove{
		source: databasePath,
		target: filepath.Join(parent, backupBase),
	})
	return moves, nil
}

// moveLegacyDatabaseFileSet moves sidecars before the main database so a
// partial move either leaves the original main path intact or puts the complete
// set in the backup namespace, and flushes the sidecar renames before the main
// database moves so a durable main database in the backup namespace always
// implies that its complete set moved with it. On failure it restores every
// move already made.
func moveLegacyDatabaseFileSet(databasePath, backupPath string) error {
	moves, err := legacyMigrationFileMoves(databasePath, backupPath)
	if err != nil {
		return fmt.Errorf("list legacy database files for backup: %w", err)
	}
	parent := filepath.Dir(databasePath)
	applied := make([]migrationFileMove, 0, len(moves))
	rollback := func() error {
		var rollbackErr error
		for index := len(applied) - 1; index >= 0; index-- {
			if err := migrationRename(applied[index].target, applied[index].source); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore legacy database file %q: %w", applied[index].target, err))
			}
		}
		if rollbackErr != nil {
			return rollbackErr
		}
		// The restored names are only durable once the parent directory is
		// flushed. Recovery treats the visible file set as a complete
		// restoration and unlinks the marker, so an unsynced rollback could be
		// reverted by a power loss after the marker removal was preserved.
		if err := syncMigrationDirectory(parent); err != nil {
			return fmt.Errorf("sync restored legacy database directory: %w", err)
		}
		return nil
	}
	for index, move := range moves {
		if index > 0 && index == len(moves)-1 {
			// The main database moves last. Flushing the sidecar renames first
			// keeps the recovery invariant that a main database found in the
			// backup namespace means the whole set is there.
			if err := syncMigrationDirectory(parent); err != nil {
				return errors.Join(fmt.Errorf("sync legacy database backup directory: %w", err), rollback())
			}
		}
		if err := migrationRename(move.source, move.target); err != nil {
			return errors.Join(fmt.Errorf("back up legacy database file %q: %w", move.source, err), rollback())
		}
		applied = append(applied, move)
	}
	if err := syncMigrationDirectory(parent); err != nil {
		return fmt.Errorf("sync legacy database backup directory: %w", err)
	}
	return nil
}

// restoreLegacyDatabaseFileSet returns every member currently in the backup
// namespace to its original name, restoring sidecars first and the main
// database last so a partial restore never looks like a complete database. The
// operation is idempotent: members already restored are skipped.
func restoreLegacyDatabaseFileSet(databasePath, backupPath string) error {
	parent := filepath.Dir(databasePath)
	base := filepath.Base(databasePath)
	backupBase := filepath.Base(backupPath)
	moves := make([]migrationFileMove, 0, len(legacyMigrationSidecarSuffixes)+1)
	for _, suffix := range legacyMigrationSidecarSuffixes {
		source := filepath.Join(parent, backupBase+suffix)
		if _, err := os.Stat(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat legacy database backup sidecar %q: %w", source, err)
		}
		moves = append(moves, migrationFileMove{source: source, target: filepath.Join(parent, base+suffix)})
	}
	if _, err := os.Stat(backupPath); err == nil {
		moves = append(moves, migrationFileMove{source: backupPath, target: filepath.Join(parent, base)})
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat legacy database backup %q: %w", backupPath, err)
	}
	var restoreErr error
	for _, move := range moves {
		if err := migrationRename(move.source, move.target); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore legacy database file %q: %w", move.source, err))
		}
	}
	if restoreErr != nil {
		return restoreErr
	}
	if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
		return fmt.Errorf("sync restored legacy database directory: %w", err)
	}
	return nil
}

// restoreLegacyDatabaseSet records the restoring phase before moving backup
// members back, so an interrupted restoration is resumed by recovery instead of
// being mistaken for a completed migration, and clears the marker only after
// the complete set is back in place.
func restoreLegacyDatabaseSet(marker databaseMigrationMarker, markerPath string) error {
	marker.Phase = migrationPhaseRestoring
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		// Restoration must not start before its intent is durable: a partial
		// restore under an old phase would be mistaken for a complete set.
		return err
	}
	if err := restoreLegacyDatabaseFileSet(marker.DatabasePath, marker.BackupPath); err != nil {
		return err
	}
	// The verified staging database is no longer needed once the legacy set is
	// back in place, and the marker may only be cleared after that cleanup is
	// durable: a marker pointing at a missing staging path would leave recovery
	// with nothing to install.
	return clearDatabaseMigrationState(marker, markerPath)
}

// legacyDatabasePartiallyBackedUp reports whether some native members already
// moved into the backup namespace while the original main database is still in
// place: the crash window between the sidecar-first moves and the main move.
func legacyDatabasePartiallyBackedUp(marker databaseMigrationMarker) (bool, error) {
	// A complete backup always contains the main database. When it is present
	// the set moved in full, so the existing main path belongs to the installed
	// target rather than to a partial move.
	if _, err := os.Stat(marker.BackupPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	parent := filepath.Dir(marker.DatabasePath)
	backupBase := filepath.Base(marker.BackupPath)
	for _, suffix := range legacyMigrationSidecarSuffixes {
		if _, err := os.Stat(filepath.Join(parent, backupBase+suffix)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func persistedStatesEqual(left, right persistedState) (bool, error) {
	leftData, err := json.Marshal(left)
	if err != nil {
		return false, fmt.Errorf("encode source state for comparison: %w", err)
	}
	rightData, err := json.Marshal(right)
	if err != nil {
		return false, fmt.Errorf("encode target state for comparison: %w", err)
	}
	return bytes.Equal(leftData, rightData), nil
}

func recoverDatabaseMigration(databasePath string) error {
	markerPath := databaseMigrationMarkerPath(databasePath)
	data, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read migration marker: %w", err)
	}
	var marker databaseMigrationMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return fmt.Errorf("decode migration marker: %w", err)
	}
	if !sameDatabasePath(marker.DatabasePath, databasePath) || marker.BackupPath == "" || marker.StagingPath == "" {
		return fmt.Errorf("migration marker does not match database path %q", databasePath)
	}
	if err := validateDatabaseMigrationPaths(marker); err != nil {
		return err
	}

	switch marker.Phase {
	case migrationPhasePrepared:
		if _, err := os.Stat(databasePath); err == nil {
			// A crash between the sidecar-first moves and the main move leaves
			// the original main database in place with its sidecars already in
			// the backup namespace; those members must come back before the
			// recovery state is cleared.
			partial, err := legacyDatabasePartiallyBackedUp(marker)
			if err != nil {
				return err
			}
			if partial {
				return restoreLegacyDatabaseSet(marker, markerPath)
			}
			// Nothing is left in the backup namespace, so the live database is
			// the original legacy database and the abandoned staging copy can be
			// discarded. The visible set may still be the product of an
			// unsynced rollback from the previous process, so flush it before
			// unlinking the marker: otherwise a power loss could preserve the
			// marker removal while reverting the restoration it depends on.
			if err := syncMigrationDirectory(filepath.Dir(marker.DatabasePath)); err != nil {
				return fmt.Errorf("sync recovered migration namespace: %w", err)
			}
			return clearDatabaseMigrationState(marker, markerPath)
		}
		if _, err := os.Stat(marker.StagingPath); err == nil {
			// The legacy set was moved but the marker was not advanced. The
			// visible namespace may still be undurable: the previous process can
			// have died after the main backup rename and before its flush, so
			// make the observed state durable before publishing a phase that
			// depends on it. Otherwise a power loss could preserve the advanced
			// marker while reverting the backup rename.
			if err := syncMigrationDirectory(filepath.Dir(marker.DatabasePath)); err != nil {
				return fmt.Errorf("sync recovered migration namespace: %w", err)
			}
			// The staged database is proved readable before the installation
			// intent is published, because a staging copy this build cannot open
			// would otherwise consume the backup and only be rejected after the
			// rename.
			if err := validateStagingDatabaseVersion(marker.StagingPath); err != nil {
				return err
			}
			// installStagingDatabase records the backed-up phase before the
			// rename, so a retry after the rename finalizes the installation
			// instead of trying to roll it back.
			_, err := installStagingDatabase(marker, markerPath)
			return err
		}
		if _, err := os.Stat(marker.BackupPath); err != nil {
			return fmt.Errorf("migration is prepared but source and backup databases are missing")
		}
		// Nothing can be installed; put the moved legacy set back.
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseBackedUp:
		if _, err := os.Stat(databasePath); err == nil {
			// The installation completed (or the set was already restored);
			// the backup stays in place. The visible live database may still be
			// an unsynced rename from the previous process, so flush it before
			// unlinking the marker: a power loss between the unlink and its sync
			// would otherwise leave neither a live database nor a marker.
			if err := syncMigrationDirectory(filepath.Dir(marker.DatabasePath)); err != nil {
				return fmt.Errorf("sync recovered migration namespace: %w", err)
			}
			return clearDatabaseMigrationState(marker, markerPath)
		}
		if _, err := os.Stat(marker.StagingPath); err == nil {
			if err := validateStagingDatabaseVersion(marker.StagingPath); err != nil {
				return err
			}
			_, err := installStagingDatabase(marker, markerPath)
			return err
		}
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseRestoring:
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseInstalled:
		return clearDatabaseMigrationState(marker, markerPath)
	default:
		return fmt.Errorf("unsupported migration phase %q", marker.Phase)
	}
}

func validateDatabaseMigrationPaths(marker databaseMigrationMarker) error {
	databasePath := filepath.Clean(marker.DatabasePath)
	parent := filepath.Dir(databasePath)
	base := filepath.Base(databasePath)
	backupPath := filepath.Clean(marker.BackupPath)
	stagingPath := filepath.Clean(marker.StagingPath)
	if filepath.Dir(backupPath) != parent || !strings.HasPrefix(filepath.Base(backupPath), base+".ladybug-backup-") {
		return fmt.Errorf("migration backup path is outside the expected database sibling namespace")
	}
	if filepath.Dir(stagingPath) != parent || !strings.HasPrefix(filepath.Base(stagingPath), base+".lattice-migrate-") {
		return fmt.Errorf("migration staging path is outside the expected database sibling namespace")
	}
	return nil
}

// validateStagingDatabaseVersion proves a staged migration result is a
// database this build can read before recovery installs it at the canonical
// path. A staging database left behind by another build is otherwise only
// discovered after the rename, when the legacy backup has already been consumed
// and the rejection can no longer be undone by a retry. A staging path the
// native engine cannot open is refused for the same reason: installing it would
// consume the backup for a database that cannot be opened, and the rejection
// keeps the migration state intact so a later retry can still install it.
func validateStagingDatabaseVersion(stagingPath string) error {
	return readStateVersionReadOnly(stagingPath)
}

// syncMigrationDirectory is the directory durability hook for the migration
// protocol. Tests replace it to observe or fail namespace syncs without a real
// crash; production always runs syncDirectory.
var syncMigrationDirectory = syncDirectory

// syncDirectory flushes a directory so the namespace changes inside it
// (renames and removals that publish, move, or clear migration state) survive a
// power loss. Syncing file contents alone does not make the enclosing directory
// entries durable, so every protocol transition that depends on a rename or a
// removal must sync the parent directory before the protocol advances or
// migration success is reported.
//
// Directory fsync is not portable: on Windows os.Open succeeds for a directory
// but the handle cannot be flushed, so this is a documented no-op there and
// callers cannot rely on directory durability on that platform. On every other
// platform a genuine failure is returned to the caller.
func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %q for sync: %w", path, err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync directory %q: %w", path, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close directory %q after sync: %w", path, closeErr)
	}
	return nil
}

// removeDatabaseMigrationMarker removes the marker and makes the removal
// durable before the caller reports success.
func removeDatabaseMigrationMarker(markerPath string) error {
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove migration marker: %w", err)
	}
	if err := syncMigrationDirectory(filepath.Dir(markerPath)); err != nil {
		return fmt.Errorf("sync migration marker removal for %q: %w", markerPath, err)
	}
	return nil
}

func writeDatabaseMigrationMarker(marker databaseMigrationMarker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode migration marker: %w", err)
	}
	markerPath := databaseMigrationMarkerPath(marker.DatabasePath)
	temporary, err := os.CreateTemp(filepath.Dir(markerPath), ".yeoul-migration-*")
	if err != nil {
		return fmt.Errorf("create migration marker: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		return fmt.Errorf("install migration marker: %w", err)
	}
	// Publishing the marker is a namespace change; sync the parent so the new
	// directory entry and the marker contents become durable together.
	if err := syncMigrationDirectory(filepath.Dir(markerPath)); err != nil {
		return fmt.Errorf("sync migration marker directory: %w", err)
	}
	return nil
}

func databaseMigrationMarkerPath(databasePath string) string {
	return databasePath + ".yeoul-migration.json"
}

// acquireMigrationOwnership takes the exclusive ownership lock for one
// migration. The lock is an operating-system lock on the database's ownership
// file, so the kernel releases it when this process exits: a crash can never
// leave a lock that later opens have to reclaim, and no caller ever removes an
// ownership file on behalf of another owner.
func acquireMigrationOwnership(databasePath string) (*databaseOwnershipLock, error) {
	for attempt := 0; attempt < migrationOwnershipAttempts; attempt++ {
		ownership, err := acquireDatabaseOwnership(databasePath, true)
		if err == nil {
			return ownership, nil
		}
		if !errors.Is(err, errDatabaseOwnershipBusy) {
			return nil, err
		}
		time.Sleep(migrationOwnershipRetryDelay)
	}
	return nil, errorf(ErrStorageFailed, "another database migration is in progress", map[string]any{
		"database_path": databasePath,
	}, nil)
}
