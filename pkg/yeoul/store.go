package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

var errFactCandidateUnavailable = errors.New("fact candidate capability unavailable")

const (
	ownershipAcquireAttempts = 4
	ownershipRetryDelay      = 25 * time.Millisecond
)

type StorageDriver string

const StorageDriverLattice StorageDriver = "lattice"

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
	// openStoreExclusiveOwnership is the writable ownership an explicit
	// maintenance window holds. Every other open is refused for the window's
	// whole lifetime, so the state a maintenance operation reads cannot be
	// changed between deciding on a change and applying it.
	openStoreExclusiveOwnership
)

func openStateStore(cfg Config) (stateStore, error) {
	return openStateStoreWithOwnership(cfg, openStoreSharedOwnership)
}

// openStateStoreWithOwnership opens the store while holding ownership of the
// requested kind. An ordinary open shares the database with every other store;
// a maintenance window owns it exclusively for the whole operation.
func openStateStoreWithOwnership(cfg Config, ownership openStoreOwnership) (stateStore, error) {
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
	if info, err := os.Stat(databasePath); err == nil && !info.IsDir() {
		return nil, errors.Join(errorf(ErrNotSupported, "database path is a regular file; refusing to open or replace it", map[string]any{
			"database_path": databasePath,
		}, nil), lock.Release())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errorf(ErrStorageFailed, "inspect database path", map[string]any{
			"database_path": databasePath,
		}, err), lock.Release())
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
// store's lifetime: the shared lock for an ordinary open, and the exclusive
// lock for a maintenance window.
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
// The exclusive lock is only granted while no other store holds the database, so
// a maintenance window that cannot take it reports the refusal instead of
// reading state another owner may still change.
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
	case "", StorageDriverLattice:
		return newLatticeStore(cfg)
	default:
		return nil, errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{
			"driver": cfg.Driver,
		}, nil)
	}
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
