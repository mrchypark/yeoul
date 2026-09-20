package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	// openStoreAttempts bounds the retries an open may spend on recovery or on
	// converting a legacy database before it gives up.
	openStoreAttempts = 4
	// ownershipAcquireAttempts and ownershipRetryDelay bound how long an open
	// waits for a contended ownership lock before reporting a running migration.
	ownershipAcquireAttempts = 4
	ownershipRetryDelay      = 25 * time.Millisecond
)

type StorageDriver string

const (
	StorageDriverLattice StorageDriver = "lattice"
	// StorageDriverLadybug is retained for legacy compatibility and migration; it is never the default.
	StorageDriverLadybug StorageDriver = "ladybug"
)

type stateStore interface {
	Load() (*persistedState, error)
	Save(state persistedState) error
	Close() error
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

func openStateStore(cfg Config) (stateStore, error) {
	if cfg.InMemory {
		return memoryStore{}, nil
	}

	// Normalize the path before any ownership or marker lookup, so equivalent
	// spellings (a trailing separator or a relative path) locate the same
	// ownership file and the same migration marker instead of bypassing them.
	databasePath, err := filepath.Abs(cfg.DatabasePath)
	if err != nil {
		return nil, errorf(ErrConfigInvalid, "resolve database path", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	cfg.DatabasePath = databasePath

	// An explicit creation is allowed to bring its own directory into being.
	// This happens before ownership is acquired, because the ownership file
	// lives next to the database.
	if cfg.CreateIfMissing {
		if err := ensureDatabaseOwnershipDirectory(databasePath); err != nil {
			return nil, errorf(ErrStorageFailed, "create database directory", map[string]any{
				"database_path": databasePath,
			}, err)
		}
	}

	// One attempt may be spent converting a legacy database, which has to
	// happen while this open holds no ownership at all.
	for attempt := 0; attempt < openStoreAttempts; attempt++ {
		// Shared ownership is held across the driver open and the store's whole
		// lifetime. A migration needs the exclusive lock, so it can neither
		// snapshot a database this store will keep changing nor install a
		// replacement underneath it.
		ownership, err := acquireOpenOwnership(cfg, databasePath)
		if err != nil {
			return nil, err
		}

		// A marker can only exist while a migration holds the exclusive lock or
		// after one crashed. Holding the shared lock rules out a live migration,
		// so a marker here means recovery is due. Recovery changes the database
		// namespace, so it runs under the exclusive lock instead.
		pending, pendingErr := migrationRecoveryPending(databasePath)
		if pendingErr != nil {
			_ = ownership.Release()
			return nil, pendingErr
		}
		if pending {
			retry, recoverErr := recoverPendingMigration(ownership, cfg, databasePath)
			if recoverErr != nil {
				return nil, recoverErr
			}
			if retry {
				time.Sleep(ownershipRetryDelay)
			}
			continue
		}

		store, openErr := openDriverStore(cfg)
		if openErr == nil {
			return &ownershipStore{stateStore: store, ownership: ownership, databasePath: databasePath}, nil
		}
		if releaseErr := ownership.Release(); releaseErr != nil {
			return nil, errors.Join(openErr, releaseErr)
		}
		if !openMayRequireMigration(cfg) {
			return nil, openErr
		}
		// The driver was left to the default, the path exists, and the canonical
		// engine could not read it: convert the legacy database and retry.
		if _, migrationErr := MigrateDatabase(context.Background(), databasePath); migrationErr != nil {
			return nil, errors.Join(openErr, migrationErr)
		}
	}
	return nil, errorf(ErrStorageFailed, "database ownership could not be established", map[string]any{
		"database_path": databasePath,
	}, nil)
}

// ownershipStore keeps the shared database ownership lock alive for the whole
// lifetime of a store. Closing the store releases it, and the operating system
// releases it even when the process exits without closing.
type ownershipStore struct {
	stateStore
	ownership    *databaseOwnershipLock
	databasePath string
}

func (s *ownershipStore) Close() error {
	return errors.Join(s.stateStore.Close(), s.ownership.Release())
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

// acquireOpenOwnership takes the shared ownership lock that an open holds for
// the store's lifetime.
//
// A contended lock means a migration owns the database. A migration holds it for
// the whole conversion, far longer than the retry window below, so the retries
// only absorb the moment another opener spends recovering an interrupted
// migration.
//
// A read-only open never proceeds without ownership. Ownership that cannot be
// established may mean another process is migrating this database right now, and
// an open without the lock could read a half-converted database or install a
// recovery over a live one, so the failure is reported instead.
func acquireOpenOwnership(cfg Config, databasePath string) (*databaseOwnershipLock, error) {
	_ = cfg
	for attempt := 0; attempt < ownershipAcquireAttempts; attempt++ {
		ownership, err := acquireDatabaseOwnership(databasePath, false)
		if err == nil {
			return ownership, nil
		}
		if !errors.Is(err, errDatabaseOwnershipBusy) {
			return nil, err
		}
		time.Sleep(ownershipRetryDelay)
	}
	return nil, migrationInProgressError(databasePath)
}

// migrationRecoveryPending reports whether an interrupted migration left a
// marker that recovery has to complete before a driver open can observe the
// database.
func migrationRecoveryPending(databasePath string) (bool, error) {
	if _, err := os.Stat(databaseMigrationMarkerPath(databasePath)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// recoverPendingMigration completes an interrupted migration under exclusive
// ownership, so no other opener can observe the half-finished protocol while it
// runs. A crash after the source backup rename leaves the marker, the backup,
// and the staging database behind without the original path, and an open that
// created a fresh database there would strand the only complete snapshot in
// staging.
//
// The caller's shared lock is released first, because the exclusive lock that
// recovery requires cannot be taken while this process still holds the shared
// one. Ownership is therefore acquired again, and recovery only runs when that
// succeeds: recovery moves and replaces files, so it must never run without
// ownership. A database whose ownership cannot be established keeps its pending
// marker and reports the failure, which is safer than installing a recovery over
// a database another process may be migrating.
//
// It reports retry when another opener is already recovering this database.
func recoverPendingMigration(shared *databaseOwnershipLock, cfg Config, databasePath string) (bool, error) {
	_ = cfg
	if err := shared.Release(); err != nil {
		return false, err
	}
	exclusive, err := acquireDatabaseOwnership(databasePath, true)
	if errors.Is(err, errDatabaseOwnershipBusy) {
		return true, nil
	}
	if err != nil {
		return false, errorf(ErrStorageFailed, "recover interrupted database migration", map[string]any{
			"database_path": databasePath,
			"reason":        "database ownership could not be established",
		}, err)
	}
	recoverErr := recoverDatabaseMigration(databasePath)
	releaseErr := exclusive.Release()
	if recoverErr != nil {
		return false, errorf(ErrStorageFailed, "recover interrupted database migration", map[string]any{
			"database_path": databasePath,
		}, recoverErr)
	}
	return false, releaseErr
}

func migrationInProgressError(databasePath string) error {
	return errorf(ErrStorageFailed, "database migration is in progress", map[string]any{
		"database_path": databasePath,
	}, nil)
}

func openDriverStore(cfg Config) (stateStore, error) {
	switch resolveStorageDriver(cfg) {
	case StorageDriverLattice:
		return newLatticeStore(cfg)
	case StorageDriverLadybug:
		return newLadybugStore(cfg)
	default:
		return nil, errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{
			"driver": cfg.Driver,
		}, nil)
	}
}

// openMayRequireMigration reports whether a failed driver open can mean the
// database is still a legacy database that the default driver has to convert.
// An explicit driver never triggers an implicit migration.
func openMayRequireMigration(cfg Config) bool {
	if cfg.Driver != "" {
		return false
	}
	if _, err := os.Stat(cfg.DatabasePath); err != nil {
		return false
	}
	return true
}

func resolveStorageDriver(cfg Config) StorageDriver {
	if cfg.Driver != "" {
		return cfg.Driver
	}
	return StorageDriverLattice
}

func emptyPersistedState() persistedState {
	return persistedState{
		Version:             1,
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
