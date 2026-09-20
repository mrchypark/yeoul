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
	"slices"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const (
	migrationPhasePrepared  = "prepared"
	migrationPhaseBackedUp  = "backed_up"
	migrationPhaseInstalled = "installed"

	legacyMigrationHelperEnv = "YEOUL_LEGACY_MIGRATION_HELPER"
)

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
		if restoreErr := restoreLegacyDatabaseFileSet(databasePath, backupPath); restoreErr != nil {
			return nil, errors.Join(err, fmt.Errorf("restore legacy database backup: %w", restoreErr))
		}
		_ = os.Remove(databaseMigrationMarkerPath(databasePath))
		return nil, err
	}
	if err := os.Rename(stagingPath, databasePath); err != nil {
		rollbackErr := restoreLegacyDatabaseFileSet(databasePath, backupPath)
		if rollbackErr != nil {
			return nil, errors.Join(fmt.Errorf("install lattice database: %w", err), fmt.Errorf("restore legacy database backup: %w", rollbackErr))
		}
		_ = os.Remove(databaseMigrationMarkerPath(databasePath))
		return nil, fmt.Errorf("install lattice database: %w", err)
	}
	marker.Phase = migrationPhaseInstalled
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		return nil, err
	}
	if err := os.Remove(databaseMigrationMarkerPath(databasePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove migration marker: %w", err)
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

// legacyMigrationFileMoves lists existing Ladybug files and sidecars that must
// move with the database. The migration marker, staging database, and any
// existing backup namespace entries are never part of the legacy file set.
func legacyMigrationFileMoves(databasePath, backupPath string) ([]migrationFileMove, error) {
	parent := filepath.Dir(databasePath)
	base := filepath.Base(databasePath)
	backupBase := filepath.Base(backupPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	var sidecars []migrationFileMove
	var mainMove *migrationFileMove
	for _, entry := range entries {
		name := entry.Name()
		if name != base && !strings.HasPrefix(name, base+".") {
			continue
		}
		if name == base+".yeoul-migration.json" ||
			strings.HasPrefix(name, base+".lattice-migrate-") ||
			strings.HasPrefix(name, base+".ladybug-backup-") {
			continue
		}
		// The engine recreates <base>.lock handles on each open; they are not
		// part of the committed Ladybug data set.
		if name == base+".lock" {
			continue
		}
		move := migrationFileMove{
			source: filepath.Join(parent, name),
			target: filepath.Join(parent, backupBase+strings.TrimPrefix(name, base)),
		}
		if name == base {
			mainMove = &move
		} else {
			sidecars = append(sidecars, move)
		}
	}
	slices.SortFunc(sidecars, func(left, right migrationFileMove) int {
		return strings.Compare(left.source, right.source)
	})
	moves := make([]migrationFileMove, 0, len(sidecars)+1)
	moves = append(moves, sidecars...)
	if mainMove != nil {
		moves = append(moves, *mainMove)
	}
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
	return nil
}

// restoreLegacyDatabaseFileSet returns every member currently in the backup
// namespace to its original name, restoring the complete native legacy file
// set after a failed or interrupted migration.
func restoreLegacyDatabaseFileSet(databasePath, backupPath string) error {
	parent := filepath.Dir(databasePath)
	base := filepath.Base(databasePath)
	backupBase := filepath.Base(backupPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("list legacy database backup files: %w", err)
	}
	var moves []migrationFileMove
	for _, entry := range entries {
		name := entry.Name()
		if name != backupBase && !strings.HasPrefix(name, backupBase+".") {
			continue
		}
		moves = append(moves, migrationFileMove{
			source: filepath.Join(parent, name),
			target: filepath.Join(parent, base+strings.TrimPrefix(name, backupBase)),
		})
	}
	if len(moves) == 0 {
		return nil
	}
	slices.SortFunc(moves, func(left, right migrationFileMove) int {
		return strings.Compare(left.source, right.source)
	})
	var restoreErr error
	for _, move := range moves {
		if err := os.Rename(move.source, move.target); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore legacy database file %q: %w", move.source, err))
		}
	}
	return restoreErr
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
			var recoverErr error
			if err := os.RemoveAll(marker.StagingPath); err != nil {
				recoverErr = errors.Join(recoverErr, fmt.Errorf("remove abandoned migration staging database: %w", err))
			}
			if err := restoreLegacyDatabaseFileSet(databasePath, marker.BackupPath); err != nil {
				recoverErr = errors.Join(recoverErr, err)
			}
			if recoverErr != nil {
				return recoverErr
			}
			return os.Remove(markerPath)
		}
		if _, err := os.Stat(marker.StagingPath); err == nil {
			if err := os.Rename(marker.StagingPath, databasePath); err != nil {
				return fmt.Errorf("resume prepared lattice database install: %w", err)
			}
			return os.Remove(markerPath)
		}
		restoreErr := restoreLegacyDatabaseFileSet(databasePath, marker.BackupPath)
		if _, err := os.Stat(databasePath); err != nil {
			return errors.Join(fmt.Errorf("migration is prepared but source and backup databases are missing"), restoreErr)
		}
		if restoreErr != nil {
			return restoreErr
		}
		return os.Remove(markerPath)
	case migrationPhaseBackedUp:
		if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(marker.StagingPath); err == nil {
				if err := os.Rename(marker.StagingPath, databasePath); err != nil {
					return fmt.Errorf("resume lattice database install: %w", err)
				}
				return os.Remove(markerPath)
			}
			if err := restoreLegacyDatabaseFileSet(databasePath, marker.BackupPath); err != nil {
				return err
			}
		}
		return os.Remove(markerPath)
	case migrationPhaseInstalled:
		return os.Remove(markerPath)
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
	return nil
}

func databaseMigrationMarkerPath(databasePath string) string {
	return databasePath + ".yeoul-migration.json"
}
