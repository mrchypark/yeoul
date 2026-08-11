package ladybug

import (
	"fmt"

	lbug "github.com/LadybugDB/go-ladybug"
)

// Store is the Ladybug database handle used by the Yeoul storage adapter.
// Raw Cypher statement generation lives in cypher.go in this package;
// callers must not construct Cypher text themselves.
type Store struct {
	db *lbug.Database
}

func Open(path string, readOnly bool) (*Store, error) {
	cfg := lbug.DefaultSystemConfig()
	cfg.ReadOnly = readOnly

	db, err := lbug.OpenDatabase(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("open ladybug database: %w", err)
	}
	return &Store{db: db}, nil
}

func OpenInMemory() (*Store, error) {
	cfg := lbug.DefaultSystemConfig()
	db, err := lbug.OpenInMemoryDatabase(cfg)
	if err != nil {
		return nil, fmt.Errorf("open in-memory ladybug database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() {
	if s == nil || s.db == nil {
		return
	}
	s.db.Close()
}

func (s *Store) Query(query string) (*lbug.QueryResult, error) {
	conn, err := lbug.OpenConnection(s.db)
	if err != nil {
		return nil, fmt.Errorf("open connection: %w", err)
	}
	defer conn.Close()

	result, err := conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query ladybug: %w", err)
	}
	return result, nil
}
