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
	if err := recoverDatabaseMigration(databasePath); err != nil {
		return nil, err
	}

	if store, err := newLatticeStore(Config{DatabasePath: databasePath, ReadOnly: true}); err == nil {
		_, loadErr := store.Load()
		closeErr := store.Close()
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
		return migrateDatabaseWithLegacyHelper(ctx, databasePath)
	}

	return migrateLegacyDatabaseInProcess(databasePath)
}

func migrateDatabaseWithLegacyHelper(ctx context.Context, databasePath string) (*DatabaseMigrationResult, error) {
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
		result.SourceDriver != string(StorageDriverLadybug) ||
		result.TargetDriver != string(StorageDriverLattice) ||
		!result.Migrated || result.BackupPath == "" {
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
	defer func() {
		_ = os.RemoveAll(stagingPath)
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
		return nil, err
	}
	marker.Phase = migrationPhaseBackedUp
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		if restoreErr := restoreLegacyDatabaseSet(marker, databaseMigrationMarkerPath(databasePath)); restoreErr != nil {
			return nil, errors.Join(err, fmt.Errorf("restore legacy database backup: %w", restoreErr))
		}
		return nil, err
	}
	if err := os.Rename(stagingPath, databasePath); err != nil {
		rollbackErr := restoreLegacyDatabaseSet(marker, databaseMigrationMarkerPath(databasePath))
		if rollbackErr != nil {
			return nil, errors.Join(fmt.Errorf("install lattice database: %w", err), fmt.Errorf("restore legacy database backup: %w", rollbackErr))
		}
		return nil, fmt.Errorf("install lattice database: %w", err)
	}
	if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
		return nil, fmt.Errorf("sync installed lattice database directory: %w", err)
	}
	marker.Phase = migrationPhaseInstalled
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		return nil, err
	}
	if err := removeDatabaseMigrationMarker(databaseMigrationMarkerPath(databasePath)); err != nil {
		return nil, err
	}
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
// set in the backup namespace. On failure it restores every move already made.
func moveLegacyDatabaseFileSet(databasePath, backupPath string) error {
	moves, err := legacyMigrationFileMoves(databasePath, backupPath)
	if err != nil {
		return fmt.Errorf("list legacy database files for backup: %w", err)
	}
	applied := make([]migrationFileMove, 0, len(moves))
	for _, move := range moves {
		if err := os.Rename(move.source, move.target); err != nil {
			var rollbackErr error
			for index := len(applied) - 1; index >= 0; index-- {
				if err := os.Rename(applied[index].target, applied[index].source); err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore legacy database file %q: %w", applied[index].target, err))
				}
			}
			return errors.Join(fmt.Errorf("back up legacy database file %q: %w", move.source, err), rollbackErr)
		}
		applied = append(applied, move)
	}
	if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
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
		if err := os.Rename(move.source, move.target); err != nil {
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
	// back in place; keep the marker when cleanup fails so recovery can retry.
	if err := os.RemoveAll(marker.StagingPath); err != nil {
		return fmt.Errorf("remove abandoned migration staging database: %w", err)
	}
	if err := syncMigrationDirectory(filepath.Dir(marker.DatabasePath)); err != nil {
		return fmt.Errorf("sync migration staging cleanup: %w", err)
	}
	return removeDatabaseMigrationMarker(markerPath)
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
	if marker.DatabasePath != databasePath || marker.BackupPath == "" || marker.StagingPath == "" {
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
			// Nothing was moved: remove only our staging directory and the
			// marker.
			if err := os.RemoveAll(marker.StagingPath); err != nil {
				return fmt.Errorf("remove abandoned migration staging database: %w", err)
			}
			if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
				return fmt.Errorf("sync migration staging cleanup: %w", err)
			}
			return removeDatabaseMigrationMarker(markerPath)
		}
		if _, err := os.Stat(marker.StagingPath); err == nil {
			// The legacy set was moved but the marker was not advanced. Record
			// the installation intent before renaming staging into place, so a
			// retry after the rename finalizes the installation instead of
			// trying to roll it back.
			marker.Phase = migrationPhaseBackedUp
			if err := writeDatabaseMigrationMarker(marker); err != nil {
				return err
			}
			if err := os.Rename(marker.StagingPath, databasePath); err != nil {
				return fmt.Errorf("resume prepared lattice database install: %w", err)
			}
			if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
				return fmt.Errorf("sync resumed lattice database install: %w", err)
			}
			marker.Phase = migrationPhaseInstalled
			if err := writeDatabaseMigrationMarker(marker); err != nil {
				return err
			}
			return removeDatabaseMigrationMarker(markerPath)
		}
		if _, err := os.Stat(marker.BackupPath); err != nil {
			return fmt.Errorf("migration is prepared but source and backup databases are missing")
		}
		// Nothing can be installed; put the moved legacy set back.
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseBackedUp:
		if _, err := os.Stat(databasePath); err == nil {
			// The installation completed (or the set was already restored);
			// the backup stays in place.
			return removeDatabaseMigrationMarker(markerPath)
		}
		if _, err := os.Stat(marker.StagingPath); err == nil {
			if err := os.Rename(marker.StagingPath, databasePath); err != nil {
				return fmt.Errorf("resume lattice database install: %w", err)
			}
			if err := syncMigrationDirectory(filepath.Dir(databasePath)); err != nil {
				return fmt.Errorf("sync resumed lattice database install: %w", err)
			}
			marker.Phase = migrationPhaseInstalled
			if err := writeDatabaseMigrationMarker(marker); err != nil {
				return err
			}
			return removeDatabaseMigrationMarker(markerPath)
		}
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseRestoring:
		return restoreLegacyDatabaseSet(marker, markerPath)
	case migrationPhaseInstalled:
		return removeDatabaseMigrationMarker(markerPath)
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
