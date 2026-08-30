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
	if err := os.Rename(databasePath, backupPath); err != nil {
		return nil, fmt.Errorf("back up legacy database: %w", err)
	}
	marker.Phase = migrationPhaseBackedUp
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		_ = os.Rename(backupPath, databasePath)
		return nil, err
	}
	if err := os.Rename(stagingPath, databasePath); err != nil {
		rollbackErr := os.Rename(backupPath, databasePath)
		if rollbackErr != nil {
			return nil, errors.Join(fmt.Errorf("install lattice database: %w", err), fmt.Errorf("restore legacy database: %w", rollbackErr))
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
			if err := os.RemoveAll(marker.StagingPath); err != nil {
				return fmt.Errorf("remove abandoned migration staging database: %w", err)
			}
			return os.Remove(markerPath)
		}
		if _, err := os.Stat(marker.BackupPath); err != nil {
			return fmt.Errorf("migration is prepared but source and backup databases are missing")
		}
		if err := os.Rename(marker.StagingPath, databasePath); err != nil {
			return fmt.Errorf("resume prepared lattice database install: %w", err)
		}
		return os.Remove(markerPath)
	case migrationPhaseBackedUp:
		if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(marker.StagingPath, databasePath); err != nil {
				return fmt.Errorf("resume lattice database install: %w", err)
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
