package yeoul

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

const migrationDeltaLimit = 64 << 10

var migrationBeforePublishBarrier func()

// MigrateLatticeToRhiza copies a closed Lattice-backed Yeoul database to a new
// Rhiza path. The source is opened read-only and is never modified.
func MigrateLatticeToRhiza(ctx context.Context, sourcePath, targetPath string) (retErr error) {
	if !rhizastore.Supported() {
		return errorf(ErrNotSupported, "Rhiza migration is unavailable on this platform", nil, rhizastore.ErrUnsupported)
	}
	if !storageMigrationDurable() {
		return errorf(ErrNotSupported, "crash-safe Lattice-to-Rhiza migration requires directory fsync, which is unavailable on Windows", nil, nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sourcePath, err := normalizedStoragePath(sourcePath)
	if err != nil {
		return err
	}
	targetPath, err = normalizedStoragePath(targetPath)
	if err != nil {
		return err
	}
	if storagePathsOverlap(sourcePath, targetPath) {
		return errorf(ErrConfigInvalid, "source and target database paths must not alias or contain one another", map[string]any{"source": sourcePath, "target": targetPath}, nil)
	}
	if _, err := os.Lstat(targetPath); err == nil {
		return errorf(ErrStorageFailed, "migration target already exists", map[string]any{"target": targetPath}, nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return errorf(ErrStorageFailed, "inspect migration target", map[string]any{"target": targetPath}, err)
	}
	if err := ensureDatabaseOwnershipDirectory(targetPath); err != nil {
		return err
	}
	targetOwnership, err := acquireDatabaseOwnership(targetPath, true)
	if err != nil {
		return errorf(ErrStorageFailed, "claim migration target", map[string]any{"target": targetPath}, err)
	}
	defer targetOwnership.Release()
	if _, err := os.Lstat(targetPath); err == nil {
		return errorf(ErrStorageFailed, "migration target already exists", map[string]any{"target": targetPath}, nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return errorf(ErrStorageFailed, "recheck migration target", map[string]any{"target": targetPath}, err)
	}
	marker := databaseMigrationMarkerPath(targetPath)
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errorf(ErrStorageFailed, "create pending migration marker", map[string]any{"marker": marker}, err)
	}
	markerData, _ := json.Marshal(map[string]string{"source": sourcePath, "target": targetPath, "state": "pending"})
	_, writeErr := file.Write(append(markerData, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr, syncStorageDirectory(filepath.Dir(marker))); err != nil {
		return errorf(ErrStorageFailed, "persist pending migration marker", map[string]any{"marker": marker}, err)
	}

	source, err := openStateStoreWithOwnership(Config{Driver: StorageDriverLattice, DatabasePath: sourcePath, ReadOnly: true}, openStoreExclusiveOwnership)
	if err != nil {
		return err
	}
	sourceClosed := false
	defer func() {
		if sourceClosed {
			return
		}
		retErr = errors.Join(retErr, source.Close())
	}()
	sourceState, loadErr := source.Load()
	if loadErr != nil {
		return loadErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Mkdir(targetPath, 0o700); err != nil {
		return errorf(ErrStorageFailed, "create migration target", map[string]any{"target": targetPath}, err)
	}
	if err := syncStorageDirectory(filepath.Dir(targetPath)); err != nil {
		return errorf(ErrStorageFailed, "sync migration target parent", map[string]any{"target": targetPath}, err)
	}
	targetCfg := Config{Driver: StorageDriverRhiza, DatabasePath: targetPath, CreateIfMissing: true}
	store, err := newRhizaStore(targetCfg)
	if err != nil {
		return err
	}
	if err := seedRhizaInBoundedDeltas(ctx, store.(*rhizaStore), *sourceState); err != nil {
		_ = store.Close()
		return err
	}
	seeded, loadErr := store.Load()
	closeTargetErr := store.Close()
	if err := errors.Join(loadErr, closeTargetErr); err != nil {
		return err
	}
	if !reflect.DeepEqual(*sourceState, *seeded) {
		return errorf(ErrStorageFailed, "migration verification differs before reopen", map[string]any{"source": sourcePath, "target": targetPath}, nil)
	}
	store, err = newRhizaStore(Config{Driver: StorageDriverRhiza, DatabasePath: targetPath})
	if err != nil {
		return err
	}
	reopened, loadErr := store.Load()
	closeTargetErr = store.Close()
	if err := errors.Join(loadErr, closeTargetErr); err != nil {
		return err
	}
	if !reflect.DeepEqual(*sourceState, *reopened) {
		return errorf(ErrStorageFailed, "migration verification differs after reopen", map[string]any{"source": sourcePath, "target": targetPath}, nil)
	}
	if migrationBeforePublishBarrier != nil {
		migrationBeforePublishBarrier()
	}
	if err := publishMigratedDatabase(ctx, marker, sourcePath, func() error {
		sourceClosed = true
		return source.Close()
	}); err != nil {
		return err
	}
	return nil
}

func publishMigratedDatabase(ctx context.Context, marker, sourcePath string, closeSource func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := closeSource(); err != nil {
		return errorf(ErrStorageFailed, "close migration source before publish", map[string]any{"source": sourcePath}, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(marker); err != nil {
		return errorf(ErrStorageFailed, "publish migrated database", map[string]any{"marker": marker}, err)
	}
	if err := syncStorageDirectory(filepath.Dir(marker)); err != nil {
		return errorf(ErrStorageFailed, "sync migration completion", map[string]any{"marker": marker}, err)
	}
	return nil
}

func seedRhizaInBoundedDeltas(ctx context.Context, store *rhizaStore, source persistedState) error {
	delta, err := latticeRecordDelta(emptyPersistedState(), source)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(delta))
	for key := range delta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]rhizastore.Record, 0, len(keys)+1)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind, id := splitLatticeKey(key)
		update := delta[key]
		record := rhizastore.Record{Key: kind + ":" + encodeMigrationID(id), Kind: kind, ID: id, Payload: update.payload, SearchText: update.searchText, Deleted: !update.present}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if len(encoded)+512 > migrationDeltaLimit {
			return errorf(ErrStorageFailed, "single record exceeds migration delta limit", map[string]any{"kind": kind, "id": id, "bytes": len(encoded), "limit": migrationDeltaLimit}, nil)
		}
		records = append(records, record)
	}
	meta, err := json.Marshal(rhizaMeta{Version: source.Version, Sequence: source.Sequence})
	if err != nil {
		return err
	}
	records = append(records, rhizastore.Record{
		Key: "YeoulMeta:" + encodeMigrationID("state"), Kind: "YeoulMeta", ID: "state", Payload: string(meta),
	})
	for start := 0; start < len(records); {
		end, chunkBytes := start, 0
		for end < len(records) {
			encoded, err := json.Marshal(records[end])
			if err != nil {
				return err
			}
			next := len(encoded) + 512
			if chunkBytes > 0 && chunkBytes+next > migrationDeltaLimit {
				break
			}
			chunkBytes += next
			end++
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		requestID, err := newRhizaRequestID()
		if err != nil {
			return err
		}
		if err := store.db.Apply(ctx, requestID, records[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func encodeMigrationID(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

func normalizedStoragePath(path string) (string, error) {
	if path == "" {
		return "", errorf(ErrConfigInvalid, "database path is required", nil, nil)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return resolveDatabasePathAliases(absolute)
}

func storagePathsOverlap(source, target string) bool {
	if source == target {
		return true
	}
	inside := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !startsWithPathParent(rel)
	}
	return inside(source, target) || inside(target, source)
}

func startsWithPathParent(path string) bool {
	return len(path) >= 3 && path[:3] == ".."+string(os.PathSeparator)
}
