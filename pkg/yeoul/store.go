package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	lstore "github.com/mrchypark/yeoul/internal/storage/lattice"
	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

var errFactCandidateUnavailable = errors.New("fact candidate capability unavailable")

const (
	ownershipAcquireAttempts = 4
	ownershipRetryDelay      = 25 * time.Millisecond
)

const restorePendingMarkerValue = "yeoul-rhiza-restore-pending-v1\n"

type StorageDriver string

const StorageDriverLattice StorageDriver = "lattice"
const StorageDriverRhiza StorageDriver = "rhiza"

type stateStore interface {
	Load() (*persistedState, error)
	// Save consumes state synchronously and must not mutate or retain its maps
	// or slices; it may retain an independent clone.
	Save(state persistedState) error
	Close() error
}

// factCandidateStore exposes an optional, read-only candidate source. The
// lookup layer must still apply every public filter after candidate selection.
type factCandidateStore interface {
	FactCandidates(ctx context.Context, subjectIDs, objectIDs []string) ([]string, error)
}

type factCandidateReadiness interface {
	FactCandidateReady() bool
}

type nativeFullTextStore interface {
	SearchCandidates(ctx context.Context, query string, counts map[string]int) (map[string]float64, error)
}

type checkpointStore interface {
	Checkpoint() error
}

// CheckpointDatabase flushes the canonical storage engine's WAL into its durable snapshot.
func CheckpointDatabase(ctx context.Context, databasePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := openStateStore(Config{DatabasePath: databasePath})
	if err != nil {
		return err
	}
	if _, err := store.Load(); err != nil {
		_ = store.Close()
		return err
	}
	checkpoint, ok := store.(checkpointStore)
	if !ok {
		_ = store.Close()
		return errorf(ErrNotSupported, "storage driver does not support checkpoints", map[string]any{
			"database_path": databasePath,
		}, nil)
	}
	checkpointErr := checkpoint.Checkpoint()
	return errors.Join(checkpointErr, store.Close())
}

// currentStateVersion is the only application-state version this build can read
// or write. A database persisted under any other version is rejected on open
// instead of being silently rewritten, because a version transition is only
// allowed through an explicit migration. The loader and the snapshot writer
// both use this constant so they cannot drift apart.
const currentStateVersion = 1

type persistedState struct {
	Version             int                           `json:"version"`
	Sequence            uint64                        `json:"sequence"`
	Sources             map[string]Source             `json:"sources"`
	Episodes            map[string]Episode            `json:"episodes"`
	Entities            map[string]Entity             `json:"entities"`
	Facts               map[string]Fact               `json:"facts"`
	FactRevisions       map[string]FactRevision       `json:"fact_revisions,omitempty"`
	EntityRevisions     map[string]EntityRevision     `json:"entity_revisions,omitempty"`
	MigrationWatermarks map[string]MigrationWatermark `json:"migration_watermarks,omitempty"`
}

// openStoreOwnership selects the ownership lock an open holds for its whole
// lifetime.
type openStoreOwnership int

const (
	// openStoreSharedOwnership is the lock an ordinary open holds. It is shared
	// with every other store and only excludes a migration.
	openStoreSharedOwnership openStoreOwnership = iota
	// openStoreExclusiveOwnership is held by maintenance and restore operations
	// that must keep other openers out for their full lifetime.
	openStoreExclusiveOwnership
)

func openStateStore(cfg Config) (stateStore, error) {
	if cfg.RestoreIfMissing {
		return openStateStoreWithOwnership(cfg, openStoreExclusiveOwnership)
	}
	return openStateStoreWithOwnership(cfg, openStoreSharedOwnership)
}

// openStateStoreWithOwnership opens the store while holding ownership of the
// requested kind. Ordinary opens share ownership; maintenance and restore
// operations hold it exclusively for the whole operation.
func openStateStoreWithOwnership(cfg Config, ownership openStoreOwnership) (stateStore, error) {
	if err := validateRhizaBackupConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.InMemory {
		return memoryStore{}, nil
	}

	// Normalize the path before any ownership or marker lookup, so equivalent
	// spellings (a trailing separator, a relative path, or a path reached
	// through a symlinked directory) locate the same ownership file and the
	// same migration marker instead of bypassing them.
	databasePath, err := filepath.Abs(cfg.DatabasePath)
	if err != nil {
		return nil, errorf(ErrConfigInvalid, "resolve database path", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	databasePath, err = resolveDatabasePathAliases(databasePath)
	if err != nil {
		return nil, errorf(ErrConfigInvalid, "resolve database path", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	cfg.DatabasePath = databasePath
	pendingRestore, err := rhizaRestorePending(databasePath)
	if err != nil {
		return nil, errorf(ErrStorageFailed, "inspect database restore marker", map[string]any{"database_path": databasePath}, err)
	}
	if pendingRestore && !cfg.RestoreIfMissing {
		return nil, restorePendingError(databasePath)
	}
	if !rhizastore.Supported() {
		isExistingRhiza := rhizaDatabaseHasEvidence(databasePath) && !latticeDatabaseHasEvidence(databasePath)
		if cfg.Driver == StorageDriverRhiza || (cfg.Driver == "" && isExistingRhiza) {
			return nil, errorf(ErrNotSupported, "Rhiza is unavailable on this platform", map[string]any{"database_path": databasePath}, rhizastore.ErrUnsupported)
		}
	}
	// Reject an explicit driver/signature conflict before creating even the
	// sibling ownership file. The same check runs again after ownership is held
	// to catch a path whose signature changed during acquisition.
	cfg.Driver, err = resolveStorageDriver(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.RhizaBackup != nil && cfg.Driver != StorageDriverRhiza {
		return nil, errorf(ErrConfigInvalid, "Rhiza backup requires the Rhiza storage driver", map[string]any{"driver": cfg.Driver}, nil)
	}

	// An explicit creation is allowed to bring its own directory into being.
	// This happens before ownership is acquired, because the ownership file
	// lives next to the database.
	if cfg.CreateIfMissing || cfg.RestoreIfMissing {
		if err := ensureDatabaseOwnershipDirectory(databasePath); err != nil {
			return nil, errorf(ErrStorageFailed, "create database directory", map[string]any{
				"database_path": databasePath,
			}, err)
		}
	}

	lock, err := acquireOpenOwnership(cfg, databasePath, ownership)
	if err != nil {
		return nil, err
	}
	pending, markerErr := migrationRecoveryPending(databasePath)
	if markerErr != nil {
		return nil, errors.Join(errorf(ErrStorageFailed, "inspect database migration marker", map[string]any{
			"database_path": databasePath,
		}, markerErr), lock.Release())
	}
	if pending {
		return nil, errors.Join(errorf(ErrStorageFailed, "unfinished database migration marker found; manual recovery is required", map[string]any{
			"database_path": databasePath,
		}, nil), lock.Release())
	}
	pendingRestore, markerErr = rhizaRestorePending(databasePath)
	if markerErr != nil {
		return nil, errors.Join(errorf(ErrStorageFailed, "inspect database restore marker", map[string]any{"database_path": databasePath}, markerErr), lock.Release())
	}
	if pendingRestore && !cfg.RestoreIfMissing {
		return nil, errors.Join(restorePendingError(databasePath), lock.Release())
	}
	if info, err := os.Stat(databasePath); err == nil && !info.IsDir() {
		return nil, errors.Join(errorf(ErrNotSupported, "database path is a regular file; refusing to open or replace it", map[string]any{
			"database_path": databasePath,
		}, nil), lock.Release())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errorf(ErrStorageFailed, "inspect database path", map[string]any{
			"database_path": databasePath,
		}, err), lock.Release())
	}
	cfg.Driver, err = resolveStorageDriver(cfg)
	if err != nil {
		return nil, errors.Join(err, lock.Release())
	}
	if cfg.RhizaBackup != nil && cfg.Driver != StorageDriverRhiza {
		return nil, errors.Join(errorf(ErrConfigInvalid, "Rhiza backup requires the Rhiza storage driver", map[string]any{"driver": cfg.Driver}, nil), lock.Release())
	}
	if cfg.RestoreIfMissing {
		if err := ensureRhizaRestorePending(databasePath); err != nil {
			return nil, errors.Join(errorf(ErrStorageFailed, "mark Rhiza restore pending", map[string]any{"database_path": databasePath}, err), lock.Release())
		}
	}
	store, err := openDriverStore(cfg)
	if err != nil {
		return nil, errors.Join(err, lock.Release())
	}
	return &ownershipStore{stateStore: store, ownership: lock, databasePath: databasePath}, nil
}

// ownershipStore keeps the shared database ownership lock alive for the whole
// lifetime of a store. Closing the store releases it, and the operating system
// releases it even when the process exits without closing.
type ownershipStore struct {
	stateStore
	ownership    *databaseOwnershipLock
	databasePath string
}

func (s *ownershipStore) CompleteRestore() error {
	store, ok := s.stateStore.(interface{ completeRestore() error })
	if !ok {
		return nil
	}
	return store.completeRestore()
}

func (s *ownershipStore) Close() error {
	return errors.Join(s.stateStore.Close(), s.ownership.Release())
}

func (s *ownershipStore) SearchCandidates(ctx context.Context, query string, counts map[string]int) (map[string]float64, error) {
	store, ok := s.stateStore.(nativeFullTextStore)
	if !ok {
		return nil, lstore.ErrSearchIndexUnavailable
	}
	return store.SearchCandidates(ctx, query, counts)
}

// Checkpoint forwards to the wrapped driver so a driver without checkpoint
// support keeps reporting the same unsupported error as before.
func (s *ownershipStore) Checkpoint() error {
	checkpoint, ok := s.stateStore.(checkpointStore)
	if !ok {
		return errorf(ErrNotSupported, "storage driver does not support checkpoints", map[string]any{
			"database_path": s.databasePath,
		}, nil)
	}
	return checkpoint.Checkpoint()
}

func (s *ownershipStore) FactCandidates(ctx context.Context, subjectIDs, objectIDs []string) ([]string, error) {
	candidates, ok := s.stateStore.(factCandidateStore)
	if !ok {
		return nil, errFactCandidateUnavailable
	}
	return candidates.FactCandidates(ctx, subjectIDs, objectIDs)
}

func (s *ownershipStore) FactCandidateReady() bool {
	ready, ok := s.stateStore.(factCandidateReadiness)
	return ok && ready.FactCandidateReady()
}

// acquireOpenOwnership takes the ownership lock that an open holds for the
// store's lifetime: the shared lock for an ordinary open and the exclusive
// lock for maintenance or recovery.
//
// A contended lock means a migration owns the database. A migration holds it for
// the whole conversion, far longer than the retry window below, so the retries
// only absorb the moment another opener spends recovering an interrupted
// migration.
//
// A writable open takes the exclusive lock because the native engine refuses a
// writable handle while any other handle is open, and because its version
// inspection and its writable open have to be one uninterrupted step: a
// database that another owner creates or replaces between those two steps would
// otherwise reach a writable open before this build established that it can
// read it. The exclusive lock makes the pair of steps a critical section
// against every other Yeoul owner of the same database.
//
// A read-only open never proceeds without ownership. Ownership that cannot be
// established may mean another process is migrating this database right now, and
// an open without the lock could read a half-converted database or install a
// recovery over a live one, so the failure is reported instead.
//
// The exclusive lock is only granted while no other store holds the database,
// so a maintenance or recovery operation cannot proceed beside another owner.
func acquireOpenOwnership(cfg Config, databasePath string, ownership openStoreOwnership) (*databaseOwnershipLock, error) {
	_ = cfg
	exclusive := ownership == openStoreExclusiveOwnership
	for attempt := 0; attempt < ownershipAcquireAttempts; attempt++ {
		lock, err := acquireDatabaseOwnership(databasePath, exclusive)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, errDatabaseOwnershipBusy) {
			return nil, err
		}
		time.Sleep(ownershipRetryDelay)
	}
	if exclusive {
		return nil, databaseInUseError(databasePath)
	}
	return nil, migrationInProgressError(databasePath)
}

// migrationRecoveryPending detects an interrupted conversion. Its source
// engine is no longer available, so opening must fail without changing files.
func migrationRecoveryPending(databasePath string) (bool, error) {
	_, err := os.Stat(databaseMigrationMarkerPath(databasePath))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func rhizaRestorePending(databasePath string) (bool, error) {
	path := rhizaRestorePendingMarkerPath(databasePath)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("restore marker is not a regular file")
	}
	return true, nil
}

func ensureRhizaRestorePending(databasePath string) error {
	if pending, err := rhizaRestorePending(databasePath); err != nil || pending {
		return err
	}
	path := rhizaRestorePendingMarkerPath(databasePath)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(restorePendingMarkerValue)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncStorageDirectory(filepath.Dir(path))
}

func clearRhizaRestorePending(databasePath string) error {
	path := rhizaRestorePendingMarkerPath(databasePath)
	pending, err := rhizaRestorePending(databasePath)
	if err != nil || !pending {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncStorageDirectory(filepath.Dir(path))
}

func rhizaRestorePendingMarkerPath(databasePath string) string {
	return databasePath + ".yeoul-rhiza-restore-pending"
}

func restorePendingError(databasePath string) error {
	return errorf(ErrStorageFailed, "an interrupted Rhiza restore is pending; retry with RestoreIfMissing after verifying the backup configuration", map[string]any{"database_path": databasePath}, nil)
}

func migrationInProgressError(databasePath string) error {
	return errorf(ErrStorageFailed, "database migration is in progress", map[string]any{
		"database_path": databasePath,
	}, nil)
}

// databaseInUseError reports that the database is owned by another process, so
// the exclusive ownership a maintenance operation needs cannot be established.
func databaseInUseError(databasePath string) error {
	return errorf(ErrStorageFailed, "database is owned by another process", map[string]any{
		"database_path": databasePath,
	}, nil)
}

func databaseMigrationMarkerPath(databasePath string) string {
	return databasePath + ".yeoul-migration.json"
}

func openDriverStore(cfg Config) (stateStore, error) {
	switch cfg.Driver {
	case StorageDriverLattice:
		return newLatticeStore(cfg)
	case StorageDriverRhiza:
		return newRhizaStore(cfg)
	default:
		return nil, errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{
			"driver": cfg.Driver,
		}, nil)
	}
}

func resolveStorageDriver(cfg Config) (StorageDriver, error) {
	lattice := latticeDatabaseHasEvidence(cfg.DatabasePath)
	rhiza := rhizaDatabaseHasEvidence(cfg.DatabasePath)
	if lattice && rhiza {
		return "", errorf(ErrStorageFailed, "database contains conflicting storage signatures", map[string]any{"database_path": cfg.DatabasePath}, nil)
	}
	if cfg.Driver != "" {
		if cfg.Driver != StorageDriverLattice && cfg.Driver != StorageDriverRhiza {
			return "", errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{"driver": cfg.Driver}, nil)
		}
		if (cfg.Driver == StorageDriverRhiza && lattice) || (cfg.Driver == StorageDriverLattice && rhiza) {
			return "", errorf(ErrStorageFailed, "requested storage driver conflicts with database signature", map[string]any{"database_path": cfg.DatabasePath, "driver": cfg.Driver}, nil)
		}
		if !lattice && !rhiza {
			info, err := os.Stat(cfg.DatabasePath)
			latticeCreationRoot := cfg.Driver == StorageDriverLattice && cfg.CreateIfMissing && latticeDatabaseUninitializedDirectory(cfg.DatabasePath)
			if err == nil && info.IsDir() && directoryHasEntries(cfg.DatabasePath) && !latticeCreationRoot {
				return "", errorf(ErrStorageFailed, "database has no recognized storage signature; refusing to initialize it", map[string]any{"database_path": cfg.DatabasePath}, nil)
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", errorf(ErrStorageFailed, "inspect database storage signatures", map[string]any{"database_path": cfg.DatabasePath}, err)
			}
		}
		return cfg.Driver, nil
	}
	if lattice {
		return StorageDriverLattice, nil
	}
	if rhiza {
		return StorageDriverRhiza, nil
	}
	info, err := os.Stat(cfg.DatabasePath)
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir() && (cfg.CreateIfMissing || cfg.RestoreIfMissing) && !directoryHasEntries(cfg.DatabasePath)) {
		if rhizastore.Supported() {
			return StorageDriverRhiza, nil
		}
		return StorageDriverLattice, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errorf(ErrStorageFailed, "inspect database storage signatures", map[string]any{"database_path": cfg.DatabasePath}, err)
	}
	return "", errorf(ErrStorageFailed, "database has no recognized storage signature; refusing to initialize it", map[string]any{"database_path": cfg.DatabasePath}, nil)
}

func validateRhizaBackupConfig(cfg Config) error {
	if cfg.ReadOnly && (cfg.RhizaBackup != nil || cfg.RestoreIfMissing) {
		return errorf(ErrConfigInvalid, "read-only opens cannot configure Rhiza backup or restore", nil, nil)
	}
	if cfg.RestoreIfMissing {
		if cfg.RhizaBackup == nil {
			return errorf(ErrConfigInvalid, "restore_if_missing requires Rhiza backup configuration", nil, nil)
		}
		if cfg.CreateIfMissing {
			return errorf(ErrConfigInvalid, "restore_if_missing and create_if_missing are mutually exclusive", nil, nil)
		}
	}
	if cfg.RhizaBackup == nil {
		return nil
	}
	if cfg.InMemory {
		return errorf(ErrConfigInvalid, "Rhiza backup is unavailable for in-memory databases", nil, nil)
	}
	if cfg.Driver == StorageDriverLattice {
		return errorf(ErrConfigInvalid, "Rhiza backup requires the Rhiza storage driver", map[string]any{"driver": cfg.Driver}, nil)
	}
	if !rhizastore.Supported() {
		return errorf(ErrNotSupported, "Rhiza backup is unavailable on this platform", nil, rhizastore.ErrUnsupported)
	}
	backup := cfg.RhizaBackup
	if backup.Provider != "s3" && backup.Provider != "gcs" && backup.Provider != "azure" {
		return errorf(ErrConfigInvalid, "Rhiza backup provider must be s3, gcs, or azure", map[string]any{"provider": backup.Provider}, nil)
	}
	if backup.Bucket == "" {
		return errorf(ErrConfigInvalid, "Rhiza backup bucket is required", nil, nil)
	}
	if strings.TrimSpace(backup.Prefix) == "" || backup.Prefix != strings.TrimSpace(backup.Prefix) {
		return errorf(ErrConfigInvalid, "Rhiza backup prefix must be nonblank without surrounding whitespace", nil, nil)
	}
	if backup.Provider == "azure" && backup.AzureConnectionString == "" && backup.AzureStorageAccount == "" {
		return errorf(ErrConfigInvalid, "Azure backup requires a storage account or connection string", nil, nil)
	}
	if backup.Provider == "azure" && backup.AzureStorageAccountKey != "" && backup.AzureConnectionString != "" {
		return errorf(ErrConfigInvalid, "Azure backup cannot configure both a storage account key and connection string", nil, nil)
	}
	return nil
}

func directoryHasEntries(path string) bool {
	entries, err := os.ReadDir(path)
	return err != nil || len(entries) != 0
}

func emptyPersistedState() persistedState {
	return persistedState{
		Version:             currentStateVersion,
		Sources:             make(map[string]Source),
		Episodes:            make(map[string]Episode),
		Entities:            make(map[string]Entity),
		Facts:               make(map[string]Fact),
		FactRevisions:       make(map[string]FactRevision),
		EntityRevisions:     make(map[string]EntityRevision),
		MigrationWatermarks: make(map[string]MigrationWatermark),
	}
}

type memoryStore struct{}

func (memoryStore) Load() (*persistedState, error) {
	state := emptyPersistedState()
	return &state, nil
}

func (memoryStore) Save(state persistedState) error {
	_ = state
	return nil
}

func (memoryStore) Close() error {
	return nil
}

func defaultSources(in map[string]Source) map[string]Source {
	if in != nil {
		return in
	}
	return make(map[string]Source)
}

func defaultEpisodes(in map[string]Episode) map[string]Episode {
	if in != nil {
		return in
	}
	return make(map[string]Episode)
}

func defaultEntities(in map[string]Entity) map[string]Entity {
	if in != nil {
		return in
	}
	return make(map[string]Entity)
}

func defaultFacts(in map[string]Fact) map[string]Fact {
	if in != nil {
		return in
	}
	return make(map[string]Fact)
}

func defaultFactRevisions(in map[string]FactRevision) map[string]FactRevision {
	if in != nil {
		return in
	}
	return make(map[string]FactRevision)
}

func defaultEntityRevisions(in map[string]EntityRevision) map[string]EntityRevision {
	if in != nil {
		return in
	}
	return make(map[string]EntityRevision)
}

func defaultMigrationWatermarks(in map[string]MigrationWatermark) map[string]MigrationWatermark {
	if in != nil {
		return in
	}
	return make(map[string]MigrationWatermark)
}
