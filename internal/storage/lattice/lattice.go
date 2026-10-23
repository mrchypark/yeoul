package lattice

import (
	"context"
	"errors"
	"fmt"
	"math"
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
var ErrSearchIndexUnavailable = errors.New("native search index is unavailable")

const maxFactCandidateFanout = 4096

const (
	searchIndexVersionKey = "yeoul.search.fts.version"
	searchIndexVersion    = "1"
	searchTextProperty    = "search_text"
)

var searchIndexes = []latticedb.FTSIndexDefinition{
	{Name: "yeoul_fact_search_v1", Kind: latticedb.FTSIndexNode, Scope: "Fact", Property: searchTextProperty},
	{Name: "yeoul_episode_search_v1", Kind: latticedb.FTSIndexNode, Scope: "Episode", Property: searchTextProperty},
	{Name: "yeoul_entity_search_v1", Kind: latticedb.FTSIndexNode, Scope: "Entity", Property: searchTextProperty},
}

// SearchDocument is the current canonical search text for one projected record.
type SearchDocument struct {
	Label string
	ID    string
	Text  string
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

// EnsureSearchIndexes creates the scoped FTS schema and backfills record text
// in one transaction. The version marker becomes visible only with that commit.
func (s *Store) EnsureSearchIndexes(ctx context.Context, documents []SearchDocument) error {
	if s == nil || s.db == nil {
		return latticedb.ErrDatabaseClosed
	}
	return s.db.UpdateContext(ctx, func(tx *latticedb.Tx) error {
		for _, definition := range searchIndexes {
			if err := tx.CreateFTSIndex(definition); err != nil && !errors.Is(err, latticedb.ErrAlreadyExists) {
				return err
			}
		}
		for _, document := range documents {
			nodes, err := tx.FindNodesByLabelProperty(document.Label, "id", document.ID, 2)
			if err != nil {
				return err
			}
			if len(nodes) != 1 {
				return fmt.Errorf("search document %s %q resolved to %d nodes", document.Label, document.ID, len(nodes))
			}
			if err := tx.SetProperty(nodes[0], searchTextProperty, document.Text); err != nil {
				return err
			}
		}
		return tx.PutAppMetadata([]byte(searchIndexVersionKey), []byte(searchIndexVersion))
	})
}

func (s *Store) SearchIndexReady() (bool, error) {
	if s == nil || s.db == nil {
		return false, latticedb.ErrDatabaseClosed
	}
	var ready bool
	err := s.db.View(func(tx *latticedb.Tx) error {
		var err error
		ready, err = searchIndexReady(tx)
		return err
	})
	if err != nil || !ready {
		return ready, err
	}
	for _, definition := range searchIndexes {
		_, err := s.db.FTSSearchIndexContext(context.Background(), definition.Name, "", latticedb.FTSSearchOptions{Limit: 1})
		if errors.Is(err, latticedb.ErrInvalidArgument) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return ready, err
}

func searchIndexReady(tx *latticedb.Tx) (bool, error) {
	version, ok, err := tx.GetAppMetadata([]byte(searchIndexVersionKey))
	if err != nil || !ok || string(version) != searchIndexVersion {
		return false, err
	}
	return true, nil
}

// SearchCandidates returns every current node hit; the per-label result limit
// is the full label cardinality so later Yeoul filters cannot lose candidates.
func (s *Store) SearchCandidates(ctx context.Context, query string, counts map[string]int) (map[string]float64, error) {
	if s == nil || s.db == nil {
		return nil, latticedb.ErrDatabaseClosed
	}
	ready, err := s.SearchIndexReady()
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrSearchIndexUnavailable
	}
	candidates := make(map[string]float64)
	for _, index := range searchIndexes {
		count := counts[index.Scope]
		if count < 0 {
			return nil, fmt.Errorf("negative %s search candidate count", index.Scope)
		}
		if count == 0 {
			continue
		}
		if uint64(count) > math.MaxUint32 {
			return nil, fmt.Errorf("%s search candidate count exceeds native FTS limit", index.Scope)
		}
		hits, err := s.db.FTSSearchIndexContext(ctx, index.Name, query, latticedb.FTSSearchOptions{Limit: uint32(count)})
		if err != nil {
			return nil, err
		}
		if err := s.db.View(func(tx *latticedb.Tx) error {
			for _, hit := range hits {
				node, err := tx.GetNode(hit.EntityID)
				if err != nil {
					return err
				}
				if node == nil {
					return fmt.Errorf("%s search hit %d has no node", index.Scope, hit.EntityID)
				}
				id, ok := node.Properties["id"].(string)
				if !ok || id == "" {
					return fmt.Errorf("%s search hit %d has malformed id", index.Scope, hit.EntityID)
				}
				candidates[index.Scope+"\x00"+id] = float64(hit.Score)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return candidates, nil
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
