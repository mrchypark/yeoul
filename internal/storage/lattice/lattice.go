package lattice

import (
	"context"
	"errors"
	"fmt"
	"slices"

	latticedb "github.com/mrchypark/latticedb-go"
)

// Store owns one LatticeDB handle for Yeoul's graph projection.
type Store struct {
	db *latticedb.DB
}

// ErrDatabaseLocked reports that the native engine refused an open because
// another handle owns the database. Callers that must not proceed on a
// database they could not inspect check for it instead of matching error text.
var ErrDatabaseLocked = latticedb.ErrDatabaseLocked
var ErrFactCandidateOverflow = errors.New("fact candidate adjacency exceeds bound")

const maxFactCandidateFanout = 4096

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

// FactCandidates resolves indexed entity IDs, then follows incoming endpoint
// edges directly. This keeps candidate work proportional to the requested
// adjacency rather than making the query planner scan Fact nodes.
func (s *Store) FactCandidates(ctx context.Context, subjectIDs, objectIDs []string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, latticedb.ErrDatabaseClosed
	}
	seen := make(map[string]struct{})
	err := s.db.View(func(tx *latticedb.Tx) error {
		lookup := func(entityID, edgeType string) error {
			result, err := tx.QueryContext(ctx, "MATCH (e:Entity) WHERE e.id = $id RETURN id(e) AS node_id", map[string]latticedb.Value{"id": entityID}, latticedb.QueryOptions{})
			if err != nil {
				return err
			}
			if len(result.Rows) == 0 {
				return nil
			}
			if len(result.Rows) != 1 {
				return fmt.Errorf("entity id %q resolved to %d nodes", entityID, len(result.Rows))
			}
			nodeID, ok := result.Rows[0]["node_id"].(int64)
			if !ok {
				return fmt.Errorf("entity id %q returned malformed node id %T", entityID, result.Rows[0]["node_id"])
			}
			edges, err := tx.GetIncomingEdgesByType(uint64(nodeID), edgeType, maxFactCandidateFanout+1)
			if err != nil {
				return err
			}
			if len(edges) > maxFactCandidateFanout {
				return ErrFactCandidateOverflow
			}
			for _, edge := range edges {
				if err := ctx.Err(); err != nil {
					return err
				}
				factNode, err := tx.GetNode(edge.SourceID)
				if err != nil {
					return err
				}
				if factNode == nil {
					return fmt.Errorf("%s edge source %d has no fact node", edgeType, edge.SourceID)
				}
				factID, ok := factNode.Properties["id"].(string)
				if !ok || factID == "" {
					return fmt.Errorf("%s edge source %d has malformed fact id", edgeType, edge.SourceID)
				}
				seen[factID] = struct{}{}
			}
			return nil
		}
		for _, entityID := range subjectIDs {
			if err := lookup(entityID, "SUBJECT"); err != nil {
				return err
			}
		}
		for _, entityID := range objectIDs {
			if err := lookup(entityID, "OBJECT_ENTITY"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for factID := range seen {
		out = append(out, factID)
	}
	slices.Sort(out)
	return out, nil
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
