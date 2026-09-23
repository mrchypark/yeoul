package yeoul

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	json "github.com/goccy/go-json"
	latticedb "github.com/mrchypark/latticedb-go"
	lstore "github.com/mrchypark/yeoul/internal/storage/lattice"
)

const (
	latticeMetaVersion           = "yeoul.state.version"
	latticeMetaSequence          = "yeoul.state.sequence"
	latticeMetaFactEndpointEdges = "yeoul.fact.endpoint_edges"
)

// errUnsupportedStateVersion marks the rejection of a persisted
// application-state version this build cannot read. The open path checks for it
// so an unreadable database is reported as it stands instead of being handed to
// the legacy migration fallback, which would replace it.
var errUnsupportedStateVersion = errors.New("unsupported application-state version")

// errLatticeStateVersionUnestablished marks a refusal to hand a database to a
// writable open when the read-only inspection could not establish which
// application-state version it holds. A contended database is not evidence of a
// legacy database, so the open reports the refusal instead of converting it.
var errLatticeStateVersionUnestablished = errors.New("application-state version could not be established")

// writableOpenBarrier runs between a read-only version inspection that found no
// persisted version and the writable open that follows it. Tests use it to
// interleave a competing writer into the window the protocol has to survive;
// production leaves it nil.
var writableOpenBarrier func()

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
	cfg                    Config
	store                  *lstore.Store
	lastState              persistedState
	loaded                 bool
	factEndpointEdgesReady bool
}

func newLatticeStore(cfg Config) (stateStore, error) {
	// A writable open can itself modify the database: the native engine may
	// checkpoint and replace state/WAL files while recovering an interrupted
	// write. The application-state version is therefore resolved through a
	// read-only inspection first, so a database this build cannot read is
	// rejected before any writable handle exists.
	if !cfg.ReadOnly {
		if err := inspectStateVersionBeforeWritableOpen(cfg.DatabasePath, cfg.CreateIfMissing); err != nil {
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
	// above cannot observe a database that only exists after CreateIfMissing,
	// and it cannot speak for a database that changed after it ran.
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
		if value, ok, err := tx.GetAppMetadata([]byte(latticeMetaFactEndpointEdges)); err != nil {
			return err
		} else {
			s.factEndpointEdgesReady = ok && string(value) == "1"
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
	if !s.factEndpointEdgesReady && len(state.Sources) == 0 && len(state.Episodes) == 0 && len(state.Entities) == 0 && len(state.Facts) == 0 && len(state.FactRevisions) == 0 && len(state.EntityRevisions) == 0 {
		s.factEndpointEdgesReady = true
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return &state, nil
}

// inspectStateVersionBeforeWritableOpen resolves the persisted application-state
// version through a read-only handle before any writable handle exists, so a
// writable open never performs native recovery on a database this build cannot
// read.
//
// An inspection that cannot establish the version is reported rather than
// discarded: a database whose compatibility is unknown must not be handed to a
// writable open, and the caller decides whether the failure still leaves room
// for the legacy conversion. Only a database that does not exist yet, or that
// holds no application records and no version, is allowed through, because the
// writable open is what creates or initializes it. createIfMissing carries the
// caller's creation policy into the inspection, so an uninitialized path that
// the writable open is about to initialize is not mistaken for a database whose
// version could not be established. That creation case is inspected a second
// time, so a database that appeared in the window is inspected instead of being
// handed to the writable open.
func inspectStateVersionBeforeWritableOpen(databasePath string, createIfMissing bool) error {
	if err := inspectStateVersionOnce(databasePath, createIfMissing); err != nil {
		return err
	}
	// The first inspection can only speak for the database as it was. The
	// writable open that follows it runs native recovery, which rewrites files
	// before this build can read the version again, so the version is resolved
	// once more immediately before that open: a database that appeared or was
	// filled in this window is inspected instead of being recovered.
	if writableOpenBarrier != nil {
		writableOpenBarrier()
	}
	return inspectStateVersionOnce(databasePath, createIfMissing)
}

// inspectStateVersionOnce returns the error that has to stop a writable open
// when the database holds a persisted application-state version this build
// cannot read, or when that version could not be established at all. A database
// that does not exist yet, or that holds no records and no version, passes when
// the caller may create it, because the writable open is then the one that
// creates or initializes it.
func inspectStateVersionOnce(databasePath string, createIfMissing bool) error {
	err := readStateVersionReadOnly(databasePath)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errUnsupportedStateVersion):
		return err
	case errors.Is(err, latticedb.ErrDatabaseLocked):
		// Another writer holds the database right now, so its persisted version
		// cannot be read. Proceeding would let a writable open run native
		// recovery on state this build may not be able to read, and waiting for
		// the writer to exit would only widen that window, so the open refuses.
		return errorf(ErrStorageFailed, "lattice database is held by another writer", map[string]any{
			"database_path": databasePath,
			"reason":        "the application-state version could not be established before a writable open",
		}, errors.Join(errLatticeStateVersionUnestablished, err))
	case createIfMissing && latticeDatabaseUninitialized(databasePath):
		// The path holds nothing the writable open could damage: it is missing,
		// or it is a directory the native engine will initialize as an empty
		// database. Deployment provisioning can create that directory ahead of
		// the first open, and an interrupted creation can leave it behind, so
		// both are the creation case rather than an unreadable database.
		return nil
	case latticeDatabaseUninitializedDirectory(databasePath):
		// The directory holds no native database, but this open may not create
		// one. A directory is not a legacy database either, because the legacy
		// engine stores a single file, so the refusal is reported instead of
		// deferring to a conversion that would replace a directory with a file.
		return errorf(ErrStorageFailed, "lattice database is not initialized", map[string]any{
			"database_path": databasePath,
			"reason":        "the directory holds no database and this open may not create one",
		}, errors.Join(errLatticeStateVersionUnestablished, err))
	}
	// The native engine could not open the file as a lattice database. The
	// failure is reported as it stands so it cannot be discarded, while the
	// caller may still convert a legacy database through the same failure.
	return err
}

// latticeDatabaseUninitialized reports whether the path is a database the
// native engine will initialize on a writable open with Create:true: a path
// that does not exist, or a directory that holds no native database files.
//
// The native engine derives its state path from the directory, so a directory
// without a state file is an empty database it will create. Anything else is
// left to the caller: a directory that holds a state file is an existing
// database whose version this build could not read, and a regular file is a
// serialized database the native engine has to open itself.
func latticeDatabaseUninitialized(databasePath string) bool {
	info, err := os.Stat(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(databasePath, "state.json"))
	return errors.Is(err, os.ErrNotExist)
}

// latticeDatabaseUninitializedDirectory reports whether the path is an existing
// directory that holds no native database. Such a directory is the creation
// case for an open that may create, and a refusal for an open that may not:
// the native engine reports it as a missing file, which is also how a database
// this build cannot read is reported, so only the on-disk state tells the two
// apart.
func latticeDatabaseUninitializedDirectory(databasePath string) bool {
	info, err := os.Stat(databasePath)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(databasePath, "state.json"))
	return errors.Is(err, os.ErrNotExist)
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
	prev := emptyPersistedState()
	if s.loaded {
		prev = s.lastState
	}
	if reflect.DeepEqual(prev, state) {
		return nil
	}
	delta, err := latticeRecordDelta(prev, state)
	if err != nil {
		return errorf(ErrStorageFailed, "write lattice graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}

	err = s.store.Update(context.Background(), func(tx *latticedb.Tx) error {
		if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(state.Version))); err != nil {
			return err
		}
		if err := tx.PutAppMetadata([]byte(latticeMetaSequence), []byte(strconv.FormatUint(state.Sequence, 10))); err != nil {
			return err
		}
		if s.factEndpointEdgesReady {
			if err := tx.PutAppMetadata([]byte(latticeMetaFactEndpointEdges), []byte("1")); err != nil {
				return err
			}
		}

		nodes, err := s.reconcileNodes(tx, delta)
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

// reconcileNodes applies the changed records to the transaction and returns the
// node id of every record it touched, which the edge pass then rewrites.
func (s *latticeStore) reconcileNodes(tx *latticedb.Tx, delta map[string]latticeRecordUpdate) (map[string]uint64, error) {
	changed := make(map[string]uint64, len(delta))
	for key, update := range delta {
		label, id := splitLatticeKey(key)
		nodeID, found, err := lookupLatticeNode(tx, label, id)
		if err != nil {
			return nil, err
		}
		if !update.present {
			if found {
				if err := tx.DeleteNode(nodeID); err != nil {
					return nil, err
				}
			}
			continue
		}
		if found {
			if err := tx.SetProperty(nodeID, "payload", update.payload); err != nil {
				return nil, err
			}
		} else {
			node, err := tx.CreateNode(latticedb.CreateNodeOptions{
				Labels: []string{label},
				Properties: map[string]any{
					"id":      id,
					"payload": update.payload,
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

// latticeRecordUpdate is one record the save has to write: its marshalled
// payload, or a removal when present is false.
type latticeRecordUpdate struct {
	payload string
	present bool
}

// latticeRecordDelta returns only the records that differ between the previously
// saved state and the state being saved.
//
// The store writes one node per record, so it only needs the payload of the
// records a mutation touched. Serializing both the previous and the current
// state to discover that delta made the cost of a one-record mutation follow
// total retained history and the write lock with it. Comparing the record values
// directly instead marshals only what changed, so an unchanged record costs a
// comparison and no allocation.
func latticeRecordDelta(prev, state persistedState) (map[string]latticeRecordUpdate, error) {
	delta := make(map[string]latticeRecordUpdate)
	if err := deltaRecords(delta, "Source", prev.Sources, state.Sources); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "Episode", prev.Episodes, state.Episodes); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "Entity", prev.Entities, state.Entities); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "Fact", prev.Facts, state.Facts); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "FactRevision", prev.FactRevisions, state.FactRevisions); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "EntityRevision", prev.EntityRevisions, state.EntityRevisions); err != nil {
		return nil, err
	}
	if err := deltaRecords(delta, "YeoulMigration", prev.MigrationWatermarks, state.MigrationWatermarks); err != nil {
		return nil, err
	}
	return delta, nil
}

// deltaRecords appends the records of one label that were added, changed, or
// removed. A record present in both maps with a deeply equal value is left
// alone, and equal values always serialize to the same payload, so skipping it
// cannot hide a write the saved state needed.
func deltaRecords[T any](delta map[string]latticeRecordUpdate, label string, prev, state map[string]T) error {
	for id, value := range state {
		if previous, ok := prev[id]; ok && reflect.DeepEqual(previous, value) {
			continue
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s %q: %w", label, id, err)
		}
		delta[label+"\x00"+id] = latticeRecordUpdate{payload: string(payload), present: true}
	}
	for id := range prev {
		if _, ok := state[id]; !ok {
			delta[label+"\x00"+id] = latticeRecordUpdate{present: false}
		}
	}
	return nil
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

// FactCandidates uses the persisted fact endpoint edges as a current-state
// candidate source. It deliberately does not inspect fact payloads or create
// indexes: old/read-only databases remain readable and the caller can fall
// back when this capability is unavailable or not semantically safe.
func (s *latticeStore) FactCandidates(ctx context.Context, subjectIDs, objectIDs []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(subjectIDs) == 0 && len(objectIDs) == 0 {
		return nil, nil
	}
	if !s.factEndpointEdgesReady {
		return nil, errFactCandidateUnavailable
	}
	ids, err := s.store.FactCandidates(ctx, subjectIDs, objectIDs)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, lstore.ErrFactCandidateOverflow) {
			return nil, errFactCandidateUnavailable
		}
		return nil, errorf(ErrStorageFailed, "query fact endpoint candidates", map[string]any{"database_path": s.cfg.DatabasePath}, err)
	}
	return ids, nil
}

func (s *latticeStore) FactCandidateReady() bool {
	return s != nil && s.factEndpointEdgesReady
}
