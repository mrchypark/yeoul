package lattice

import (
	"context"
	"errors"
	"fmt"

	latticedb "github.com/mrchypark/latticedb-go"
)

// Store owns one LatticeDB handle for Yeoul's graph projection.
type Store struct {
	db *latticedb.DB
}

func Open(path string, create, readOnly bool) (*Store, error) {
	db, err := latticedb.Open(path, latticedb.OpenOptions{
		Create:   create,
		ReadOnly: readOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("open lattice database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) View(fn func(*latticedb.Tx) error) error {
	if s == nil || s.db == nil {
		return latticedb.ErrDatabaseClosed
	}
	return s.db.View(fn)
}

func (s *Store) Update(ctx context.Context, fn func(*latticedb.Tx) error) error {
	if s == nil || s.db == nil {
		return latticedb.ErrDatabaseClosed
	}
	return s.db.UpdateContext(ctx, fn)
}

func (s *Store) NodeIDs(label string) ([]uint64, error) {
	if s == nil || s.db == nil {
		return nil, latticedb.ErrDatabaseClosed
	}
	return s.db.GetNodesByLabel(label)
}

func (s *Store) EnsureNodeIDIndex(label string) error {
	if s == nil || s.db == nil {
		return latticedb.ErrDatabaseClosed
	}
	err := s.db.CreateNodePropertyIndex(label, "id")
	if errors.Is(err, latticedb.ErrAlreadyExists) {
		return nil
	}
	return err
}

func (s *Store) Checkpoint() error {
	if s == nil || s.db == nil {
		return latticedb.ErrDatabaseClosed
	}
	return s.db.Checkpoint()
}
