package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

	// A live migration owns the database until it finishes installing the
	// replacement; a stale lock from a crashed migration is reclaimed here.
	if err := reclaimStaleMigrationLock(cfg.DatabasePath); err != nil {
		return nil, err
	}
	if _, err := os.Stat(legacyMigrationLockPath(cfg.DatabasePath)); err == nil {
		return nil, errorf(ErrStorageFailed, "database migration is in progress", map[string]any{
			"database_path": cfg.DatabasePath,
		}, nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Complete an interrupted migration before any open or create attempt can
	// observe a half-renamed database. A crash after the source backup rename
	// leaves the marker, the backup, and the staging database behind without
	// the original path, and an open that creates a fresh database there would
	// strand the only complete snapshot in staging.
	//
	// The path is normalized the same way MigrateDatabase normalizes it, so
	// equivalent spellings (a trailing separator or a relative path) locate the
	// same marker instead of bypassing recovery.
	databasePath, err := filepath.Abs(cfg.DatabasePath)
	if err != nil {
		return nil, errorf(ErrConfigInvalid, "resolve database path", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	cfg.DatabasePath = databasePath

	if err := recoverDatabaseMigration(cfg.DatabasePath); err != nil {
		return nil, errorf(ErrStorageFailed, "recover interrupted database migration", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	switch resolveStorageDriver(cfg) {
	case StorageDriverLattice:
		store, err := newLatticeStore(cfg)
		if err == nil || cfg.Driver == StorageDriverLattice {
			return store, err
		}
		if _, statErr := os.Stat(cfg.DatabasePath); statErr != nil {
			return nil, err
		}
		if _, migrationErr := MigrateDatabase(context.Background(), cfg.DatabasePath); migrationErr != nil {
			return nil, errors.Join(err, migrationErr)
		}
		return newLatticeStore(cfg)
	case StorageDriverLadybug:
		return newLadybugStore(cfg)
	default:
		return nil, errorf(ErrConfigInvalid, "unsupported storage driver", map[string]any{
			"driver": cfg.Driver,
		}, nil)
	}
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
