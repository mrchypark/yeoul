package yeoul

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"

	json "github.com/goccy/go-json"
	latticedb "github.com/mrchypark/latticedb-go"
	lstore "github.com/mrchypark/yeoul/internal/storage/lattice"
)

const (
	latticeMetaVersion  = "yeoul.state.version"
	latticeMetaSequence = "yeoul.state.sequence"
)

// errUnsupportedStateVersion marks the rejection of a persisted
// application-state version this build cannot read. The open path checks for it
// so an unreadable database is reported as it stands instead of being handed to
// the legacy migration fallback, which would replace it.
var errUnsupportedStateVersion = errors.New("unsupported application-state version")

var latticeLabels = []string{
	"Source",
	"Episode",
	"Entity",
	"Fact",
	"FactRevision",
	"EntityRevision",
	"YeoulMigration",
}

type latticeStore struct {
	cfg       Config
	store     *lstore.Store
	lastState persistedState
	loaded    bool
}

func newLatticeStore(cfg Config) (stateStore, error) {
	// A writable open can itself modify the database: the native engine may
	// checkpoint and replace state/WAL files while recovering an interrupted
	// write. The application-state version is therefore resolved through a
	// read-only inspection first, so a database this build cannot read is
	// rejected before any writable handle exists.
	if !cfg.ReadOnly {
		if err := validateStateVersionReadOnly(cfg.DatabasePath, cfg.CreateIfMissing); err != nil {
			return nil, err
		}
	}
	store, err := lstore.Open(cfg.DatabasePath, cfg.CreateIfMissing, cfg.ReadOnly)
	if err != nil {
		return nil, errorf(ErrStorageFailed, "open lattice database", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	state := &latticeStore{cfg: cfg, store: store, lastState: emptyPersistedState()}
	// Re-check through the handle that will actually serve reads: the inspection
	// above cannot observe a database that only exists after CreateIfMissing.
	if err := state.validateStateVersion(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if !cfg.ReadOnly {
		for _, label := range latticeLabels {
			if err := store.EnsureNodeIDIndex(label); err != nil {
				_ = store.Close()
				return nil, errorf(ErrStorageFailed, "ensure lattice node index", map[string]any{
					"database_path": cfg.DatabasePath,
					"label":         label,
				}, err)
			}
		}
	}
	return state, nil
}

func (s *latticeStore) Load() (*persistedState, error) {
	state := emptyPersistedState()
	err := s.store.View(func(tx *latticedb.Tx) error {
		if value, ok, err := tx.GetAppMetadata([]byte(latticeMetaVersion)); err != nil {
			return err
		} else if ok {
			version, err := s.decodeStateVersion(value)
			if err != nil {
				return err
			}
			state.Version = version
		}
		if value, ok, err := tx.GetAppMetadata([]byte(latticeMetaSequence)); err != nil {
			return err
		} else if ok {
			state.Sequence, err = strconv.ParseUint(string(value), 10, 64)
			if err != nil {
				return fmt.Errorf("decode state sequence: %w", err)
			}
		}

		loaders := map[string]func(string, []byte) error{
			"Source": func(id string, payload []byte) error {
				var item Source
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.Sources[id] = item
				return nil
			},
			"Episode": func(id string, payload []byte) error {
				var item Episode
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.Episodes[id] = item
				return nil
			},
			"Entity": func(id string, payload []byte) error {
				var item Entity
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.Entities[id] = item
				return nil
			},
			"Fact": func(id string, payload []byte) error {
				var item Fact
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.Facts[id] = item
				return nil
			},
			"FactRevision": func(id string, payload []byte) error {
				var item FactRevision
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.FactRevisions[id] = item
				return nil
			},
			"EntityRevision": func(id string, payload []byte) error {
				var item EntityRevision
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.EntityRevisions[id] = item
				return nil
			},
			"YeoulMigration": func(id string, payload []byte) error {
				var item MigrationWatermark
				if err := json.Unmarshal(payload, &item); err != nil {
					return err
				}
				if item.ID != id {
					return fmt.Errorf("payload id %q does not match indexed id", item.ID)
				}
				state.MigrationWatermarks[id] = item
				return nil
			},
		}

		seen := make(map[string]struct{})
		for _, label := range latticeLabels {
			ids, err := s.store.NodeIDs(label)
			if err != nil {
				return err
			}
			for _, nodeID := range ids {
				node, err := tx.GetNode(nodeID)
				if err != nil {
					return err
				}
				if node == nil {
					continue
				}
				id, ok := node.Properties["id"].(string)
				if !ok || id == "" {
					return fmt.Errorf("%s node %d has no string id", label, nodeID)
				}
				key := label + "\x00" + id
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate %s id %q", label, id)
				}
				seen[key] = struct{}{}
				payload, err := latticePayload(node.Properties["payload"])
				if err != nil {
					return fmt.Errorf("decode %s %q payload: %w", label, id, err)
				}
				if err := loaders[label](id, payload); err != nil {
					return fmt.Errorf("decode %s %q: %w", label, id, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, errorf(ErrStorageFailed, "load lattice graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return &state, nil
}

// validateStateVersionReadOnly resolves the persisted application-state version
// through a read-only handle, so a writable open never performs native recovery
// on a database this build cannot read. A database that does not exist yet has
// no persisted version to reject, and a database the native engine cannot open
// at all is left for the real open to report.
func validateStateVersionReadOnly(databasePath string, createIfMissing bool) error {
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A database that does not exist yet has no persisted version to
			// reject. The real open reports a missing database when creation is
			// not allowed, with the error that belongs to that decision.
			return nil
		}
		return errorf(ErrStorageFailed, "inspect lattice database for state version", map[string]any{
			"database_path": databasePath,
		}, err)
	}
	if err := readStateVersionReadOnly(databasePath); err != nil {
		if errors.Is(err, errUnsupportedStateVersion) {
			return err
		}
		// The native engine cannot read the file at all; the caller's real open
		// reports that failure with its own error, so the version check stays out
		// of the way here.
		return nil
	}
	return nil
}

// readStateVersionReadOnly resolves the application-state version of an
// existing database through a read-only handle. It reports a native open or
// close failure as it stands, so a caller that must not act on an unreadable
// database can refuse instead of deferring to a later writable open.
func readStateVersionReadOnly(databasePath string) error {
	store, err := lstore.Open(databasePath, false, true)
	if err != nil {
		return errorf(ErrStorageFailed, "open lattice database for state version inspection", map[string]any{
			"database_path": databasePath,
		}, err)
	}
	state := &latticeStore{cfg: Config{DatabasePath: databasePath, ReadOnly: true}, store: store, lastState: emptyPersistedState()}
	versionErr := state.validateStateVersion()
	closeErr := store.Close()
	if versionErr != nil {
		return versionErr
	}
	if closeErr != nil {
		return errorf(ErrStorageFailed, "close lattice database after state version inspection", map[string]any{
			"database_path": databasePath,
		}, closeErr)
	}
	return nil
}

// validateStateVersion rejects a persisted application-state version this build
// does not support, before any index build, seeding, or application write. It
// also rejects a database that holds application records but no version, whose
// provenance is unknown. A database without a version and without records is
// the state a brand-new database starts in, so it is accepted.
func (s *latticeStore) validateStateVersion() error {
	var (
		value   []byte
		present bool
	)
	if err := s.store.View(func(tx *latticedb.Tx) error {
		got, ok, err := tx.GetAppMetadata([]byte(latticeMetaVersion))
		if err != nil {
			return err
		}
		value, present = got, ok
		return nil
	}); err != nil {
		return errorf(ErrStorageFailed, "read lattice state version", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	if !present {
		empty, err := latticeStateEmpty(s.store)
		if err != nil {
			return errorf(ErrStorageFailed, "read lattice state version", map[string]any{
				"database_path": s.cfg.DatabasePath,
			}, err)
		}
		if empty {
			return nil
		}
		return s.unsupportedStateVersion("missing")
	}
	_, err := s.decodeStateVersion(value)
	return err
}

// decodeStateVersion converts a persisted application-state version and rejects
// any value this build does not support, so the loader and the snapshot writer
// share one definition of the supported version.
func (s *latticeStore) decodeStateVersion(value []byte) (int, error) {
	version, err := strconv.Atoi(string(value))
	if err != nil || version != currentStateVersion {
		return 0, s.unsupportedStateVersion(string(value))
	}
	return version, nil
}

// unsupportedStateVersion reports a persisted version this build cannot read.
// The wrapped sentinel lets the open path tell this rejection apart from a
// database the legacy migration path may legitimately convert.
func (s *latticeStore) unsupportedStateVersion(found string) error {
	return errorf(ErrNotSupported, "unsupported application-state version", map[string]any{
		"database_path":     s.cfg.DatabasePath,
		"found_version":     found,
		"supported_version": currentStateVersion,
	}, errUnsupportedStateVersion)
}

// latticeStateEmpty reports whether the database holds no application records.
func latticeStateEmpty(store *lstore.Store) (bool, error) {
	for _, label := range latticeLabels {
		ids, err := store.NodeIDs(label)
		if err != nil {
			return false, err
		}
		if len(ids) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func latticePayload(value any) ([]byte, error) {
	switch value := value.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported payload type %T", value)
	}
}

func (s *latticeStore) Save(state persistedState) error {
	if s.cfg.ReadOnly {
		return nil
	}
	prev := s.lastState
	if !s.loaded {
		prev = emptyPersistedState()
	}
	if reflect.DeepEqual(prev, state) {
		return nil
	}

	err := s.store.Update(context.Background(), func(tx *latticedb.Tx) error {
		if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(state.Version))); err != nil {
			return err
		}
		if err := tx.PutAppMetadata([]byte(latticeMetaSequence), []byte(strconv.FormatUint(state.Sequence, 10))); err != nil {
			return err
		}

		nodes, err := s.reconcileNodes(tx, prev, state)
		if err != nil {
			return err
		}
		if err := replaceLatticeEdges(tx, nodes, state); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return errorf(ErrStorageFailed, "write lattice graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return nil
}

func (s *latticeStore) reconcileNodes(tx *latticedb.Tx, prev, state persistedState) (map[string]uint64, error) {
	previous, err := latticeRecords(prev)
	if err != nil {
		return nil, err
	}
	wanted, err := latticeRecords(state)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(previous)+len(wanted))
	for key := range previous {
		keys[key] = struct{}{}
	}
	for key := range wanted {
		keys[key] = struct{}{}
	}
	changed := make(map[string]uint64)
	for key := range keys {
		oldPayload, existed := previous[key]
		payload, exists := wanted[key]
		if existed && exists && oldPayload == payload {
			continue
		}
		label, id := splitLatticeKey(key)
		nodeID, found, err := lookupLatticeNode(tx, label, id)
		if err != nil {
			return nil, err
		}
		if !exists {
			if found {
				if err := tx.DeleteNode(nodeID); err != nil {
					return nil, err
				}
			}
			continue
		}
		if found {
			if err := tx.SetProperty(nodeID, "payload", payload); err != nil {
				return nil, err
			}
		} else {
			node, err := tx.CreateNode(latticedb.CreateNodeOptions{
				Labels: []string{label},
				Properties: map[string]any{
					"id":      id,
					"payload": payload,
				},
			})
			if err != nil {
				return nil, err
			}
			nodeID = node.ID
		}
		changed[key] = nodeID
	}
	return changed, nil
}

func lookupLatticeNode(tx *latticedb.Tx, label, id string) (uint64, bool, error) {
	ids, err := tx.FindNodesByLabelProperty(label, "id", id, 2)
	if err != nil {
		return 0, false, err
	}
	if len(ids) > 1 {
		return 0, false, fmt.Errorf("duplicate %s id %q", label, id)
	}
	if len(ids) == 0 {
		return 0, false, nil
	}
	return ids[0], true, nil
}

func latticeRecords(state persistedState) (map[string]string, error) {
	records := make(map[string]string)
	add := func(label, id string, value any) error {
		payload, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s %q: %w", label, id, err)
		}
		records[label+"\x00"+id] = string(payload)
		return nil
	}
	for id, item := range state.Sources {
		if err := add("Source", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.Episodes {
		if err := add("Episode", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.Entities {
		if err := add("Entity", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.Facts {
		if err := add("Fact", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.FactRevisions {
		if err := add("FactRevision", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.EntityRevisions {
		if err := add("EntityRevision", id, item); err != nil {
			return nil, err
		}
	}
	for id, item := range state.MigrationWatermarks {
		if err := add("YeoulMigration", id, item); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func splitLatticeKey(key string) (string, string) {
	for i := range len(key) {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func replaceLatticeEdges(tx *latticedb.Tx, nodes map[string]uint64, state persistedState) error {
	for _, nodeID := range nodes {
		edges, err := tx.GetOutgoingEdges(nodeID)
		if err != nil {
			return err
		}
		for _, edge := range edges {
			if err := tx.DeleteEdge(edge.SourceID, edge.TargetID, edge.Type); err != nil {
				return err
			}
		}
	}
	create := func(fromLabel, fromID, toLabel, toID, edgeType string, properties map[string]any) error {
		if fromID == "" || toID == "" {
			return nil
		}
		from, fromOK := nodes[fromLabel+"\x00"+fromID]
		to, toOK, err := lookupLatticeNode(tx, toLabel, toID)
		if !fromOK || err != nil {
			return err
		}
		if !toOK {
			return fmt.Errorf("cannot create %s edge from %s %q to %s %q", edgeType, fromLabel, fromID, toLabel, toID)
		}
		_, err = tx.CreateEdge(from, to, edgeType, latticedb.CreateEdgeOptions{Properties: properties})
		return err
	}
	for key := range nodes {
		label, id := splitLatticeKey(key)
		if label == "Episode" {
			episode, ok := state.Episodes[id]
			if ok {
				if err := create("Episode", episode.ID, "Source", episode.SourceID, "FROM_SOURCE", nil); err != nil {
					return err
				}
			}
			continue
		}
		if label != "Fact" {
			continue
		}
		fact, ok := state.Facts[id]
		if !ok {
			continue
		}
		if err := create("Fact", fact.ID, "Entity", fact.SubjectID, "SUBJECT", nil); err != nil {
			return err
		}
		if err := create("Fact", fact.ID, "Entity", fact.ObjectID, "OBJECT_ENTITY", nil); err != nil {
			return err
		}
		for _, episodeID := range fact.SupportingEpisodeIDs {
			if err := create("Fact", fact.ID, "Episode", episodeID, "SUPPORTED_BY", nil); err != nil {
				return err
			}
		}
		for _, previousID := range metadataStringIDs(fact.Metadata["supersedes"]) {
			properties := map[string]any{}
			if reason, ok := fact.Metadata["supersede_reason"].(string); ok && reason != "" {
				properties["reason"] = reason
			}
			if err := create("Fact", fact.ID, "Fact", previousID, "SUPERSEDES", properties); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *latticeStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *latticeStore) Checkpoint() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Checkpoint()
}
