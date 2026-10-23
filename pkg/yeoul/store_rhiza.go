package yeoul

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	json "github.com/goccy/go-json"
	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

const rhizaDriverMarker = ".yeoul-storage-driver"
const rhizaDriverMarkerValue = "rhiza-catalog-v1\n"

var errStorageOutcomeUnknown = errors.New("storage commit outcome is unknown")

type rhizaStore struct {
	cfg       Config
	db        *rhizastore.DB
	lastState persistedState
	loaded    bool
	poisoned  bool
}

type rhizaMeta struct {
	Version  int    `json:"version"`
	Sequence uint64 `json:"sequence"`
}

func newRhizaStore(cfg Config) (stateStore, error) {
	if !rhizastore.Supported() {
		return nil, errorf(ErrNotSupported, "Rhiza is unavailable on this platform", map[string]any{"database_path": cfg.DatabasePath}, rhizastore.ErrUnsupported)
	}
	info, err := os.Stat(cfg.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		if !cfg.CreateIfMissing {
			return nil, errorf(ErrStorageFailed, "Rhiza database does not exist", map[string]any{"database_path": cfg.DatabasePath}, err)
		}
		if err := os.Mkdir(cfg.DatabasePath, 0o700); err != nil {
			return nil, errorf(ErrStorageFailed, "create Rhiza database directory", map[string]any{"database_path": cfg.DatabasePath}, err)
		}
		info = nil
	} else if err != nil {
		return nil, errorf(ErrStorageFailed, "inspect Rhiza database directory", map[string]any{"database_path": cfg.DatabasePath}, err)
	}
	if info != nil && !info.IsDir() {
		return nil, errorf(ErrNotSupported, "Rhiza database path is not a directory", map[string]any{"database_path": cfg.DatabasePath}, nil)
	}
	_, markerExists, markerErr := readRhizaDriverMarker(cfg.DatabasePath)
	if markerErr != nil {
		return nil, errorf(ErrStorageFailed, "inspect Rhiza storage marker", map[string]any{"database_path": cfg.DatabasePath}, markerErr)
	}
	if !markerExists {
		entries, readErr := os.ReadDir(cfg.DatabasePath)
		if readErr != nil {
			return nil, errorf(ErrStorageFailed, "inspect unmarked Rhiza directory", map[string]any{"database_path": cfg.DatabasePath}, readErr)
		}
		if len(entries) != 0 {
			return nil, errorf(ErrStorageFailed, "unmarked nonempty directory is not a recognized Rhiza database", map[string]any{"database_path": cfg.DatabasePath}, nil)
		}
	}
	nativeData := rhizaNativeDatabaseHasEvidence(cfg.DatabasePath)
	if markerExists && !nativeData {
		entries, readErr := os.ReadDir(cfg.DatabasePath)
		if readErr != nil {
			return nil, errorf(ErrStorageFailed, "inspect marker-only Rhiza directory", map[string]any{"database_path": cfg.DatabasePath}, readErr)
		}
		if !onlyRhizaMarker(entries) || !cfg.CreateIfMissing {
			return nil, errorf(ErrStorageFailed, "Rhiza marker exists without native data; refusing to initialize unless explicitly creating an empty directory", map[string]any{"database_path": cfg.DatabasePath}, nil)
		}
	}
	if err := ensureRhizaDriverMarker(cfg.DatabasePath, cfg.CreateIfMissing); err != nil {
		return nil, errorf(ErrStorageFailed, "validate Rhiza database marker", map[string]any{"database_path": cfg.DatabasePath}, err)
	}
	db, err := rhizastore.Open(cfg.DatabasePath)
	if err != nil {
		return nil, errorf(ErrStorageFailed, "open Rhiza database", map[string]any{"database_path": cfg.DatabasePath}, err)
	}
	return &rhizaStore{cfg: cfg, db: db, lastState: emptyPersistedState()}, nil
}

func ensureRhizaDriverMarker(databasePath string, create bool) error {
	marker := filepath.Join(databasePath, rhizaDriverMarker)
	data, exists, err := readRhizaDriverMarker(databasePath)
	if err != nil {
		return err
	}
	if exists {
		if string(data) != rhizaDriverMarkerValue {
			return fmt.Errorf("unrecognized storage marker")
		}
		return nil
	}
	if !create {
		return fmt.Errorf("Rhiza storage marker is missing")
	}
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(rhizaDriverMarkerValue)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncStorageDirectory(databasePath)
}

func readRhizaDriverMarker(databasePath string) ([]byte, bool, error) {
	path := filepath.Join(databasePath, rhizaDriverMarker)
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !before.Mode().IsRegular() {
		return nil, false, fmt.Errorf("storage marker is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	opened, statErr := file.Stat()
	var data []byte
	var readErr error
	if statErr == nil && opened.Mode().IsRegular() && os.SameFile(before, opened) {
		data, readErr = io.ReadAll(io.LimitReader(file, int64(len(rhizaDriverMarkerValue)+1)))
		if readErr == nil && len(data) > len(rhizaDriverMarkerValue) {
			readErr = fmt.Errorf("storage marker exceeds the supported length")
		}
	} else if statErr == nil {
		statErr = fmt.Errorf("storage marker changed while opening")
	}
	closeErr := file.Close()
	after, pathErr := os.Lstat(path)
	if pathErr == nil {
		if after.Mode()&os.ModeSymlink != 0 || statErr != nil || !os.SameFile(opened, after) {
			pathErr = fmt.Errorf("storage marker changed while reading")
		}
	}
	if errors.Is(pathErr, os.ErrNotExist) {
		pathErr = fmt.Errorf("storage marker disappeared while reading")
	}
	if err := errors.Join(statErr, readErr, closeErr, pathErr); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func onlyRhizaMarker(entries []os.DirEntry) bool {
	return len(entries) == 1 && entries[0].Name() == rhizaDriverMarker && entries[0].Type().IsRegular()
}

func (s *rhizaStore) Load() (*persistedState, error) {
	rows, err := s.db.Records(context.Background())
	if err != nil {
		return nil, errorf(ErrStorageFailed, "load Rhiza graph state", map[string]any{"database_path": s.cfg.DatabasePath}, err)
	}
	state := emptyPersistedState()
	metaFound := false
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		wantKey := row.Kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(row.ID))
		if row.Key != wantKey {
			return nil, fmt.Errorf("Rhiza record key %q does not match kind and ID", row.Key)
		}
		logicalKey := row.Kind + "\x00" + row.ID
		if _, exists := seen[logicalKey]; exists {
			return nil, fmt.Errorf("duplicate Rhiza record %s %q", row.Kind, row.ID)
		}
		seen[logicalKey] = struct{}{}
		if row.Deleted {
			continue
		}
		if row.Kind == "YeoulMeta" && row.ID == "state" {
			var meta rhizaMeta
			if err := json.Unmarshal([]byte(row.Payload), &meta); err != nil {
				return nil, fmt.Errorf("decode Rhiza application metadata: %w", err)
			}
			if meta.Version != currentStateVersion {
				return nil, errorf(ErrStorageFailed, "unsupported application-state version", map[string]any{"version": meta.Version}, nil)
			}
			state.Version, state.Sequence = meta.Version, meta.Sequence
			metaFound = true
			continue
		}
		if err := decodeRhizaStateRecord(&state, row.Kind, row.ID, row.Payload); err != nil {
			return nil, err
		}
	}
	if len(rows) > 0 && !metaFound {
		return nil, fmt.Errorf("Rhiza database has records but no Yeoul state metadata")
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return &state, nil
}

func decodeRhizaStateRecord(state *persistedState, kind, id, payload string) error {
	switch kind {
	case "Source":
		var item Source
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return fmt.Errorf("decode Rhiza Source %q: %w", id, err)
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza Source key %q does not match payload ID %q", id, item.ID)
		}
		state.Sources[id] = item
	case "Episode":
		var item Episode
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza Episode key %q does not match payload ID %q", id, item.ID)
		}
		state.Episodes[id] = item
	case "Entity":
		var item Entity
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza Entity key %q does not match payload ID %q", id, item.ID)
		}
		state.Entities[id] = item
	case "Fact":
		var item Fact
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza Fact key %q does not match payload ID %q", id, item.ID)
		}
		state.Facts[id] = item
	case "FactRevision":
		var item FactRevision
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza FactRevision key %q does not match payload ID %q", id, item.ID)
		}
		state.FactRevisions[id] = item
	case "EntityRevision":
		var item EntityRevision
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza EntityRevision key %q does not match payload ID %q", id, item.ID)
		}
		state.EntityRevisions[id] = item
	case "YeoulMigration":
		var item MigrationWatermark
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return err
		}
		if item.ID != id {
			return fmt.Errorf("Rhiza YeoulMigration key %q does not match payload ID %q", id, item.ID)
		}
		state.MigrationWatermarks[id] = item
	default:
		return fmt.Errorf("unknown Rhiza record kind %q", kind)
	}
	return nil
}

func (s *rhizaStore) Save(state persistedState) error {
	if s.poisoned {
		return errStorageOutcomeUnknown
	}
	if s.cfg.ReadOnly || reflect.DeepEqual(s.lastState, state) {
		return nil
	}
	prev := s.lastState
	if !s.loaded {
		prev = emptyPersistedState()
	}
	delta, err := latticeRecordDelta(prev, state)
	if err != nil {
		return errorf(ErrStorageFailed, "prepare Rhiza graph delta", nil, err)
	}
	meta, err := json.Marshal(rhizaMeta{Version: state.Version, Sequence: state.Sequence})
	if err != nil {
		return err
	}
	delta["YeoulMeta\x00state"] = latticeRecordUpdate{payload: string(meta), present: true}
	rows := make([]rhizastore.Record, 0, len(delta))
	for composite, update := range delta {
		kind, id := splitLatticeKey(composite)
		key := kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(id))
		rows = append(rows, rhizastore.Record{Key: key, Kind: kind, ID: id, Payload: update.payload, SearchText: update.searchText, Deleted: !update.present})
	}
	requestID, err := newRhizaRequestID()
	if err != nil {
		return err
	}
	if err := s.db.Apply(context.Background(), requestID, rows); err != nil {
		var tooLarge *rhizastore.MutationTooLargeError
		if errors.As(err, &tooLarge) {
			return errorf(ErrInputInvalid, "atomic Rhiza state delta exceeds the supported mutation limit", map[string]any{
				"encoded_bytes": tooLarge.EncodedBytes,
				"limit_bytes":   tooLarge.Limit,
			}, err)
		}
		if errors.Is(err, errStorageOutcomeUnknown) || errors.Is(err, rhizastore.ErrCommitUnknown) {
			s.poisoned = true
			return errors.Join(errStorageOutcomeUnknown, err)
		}
		return errorf(ErrStorageFailed, "write Rhiza graph delta", map[string]any{"database_path": s.cfg.DatabasePath}, err)
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return nil
}

func newRhizaRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "yeoul-" + hex.EncodeToString(value[:]), nil
}

func (s *rhizaStore) SearchCandidates(ctx context.Context, query string, counts map[string]int) (map[string]float64, error) {
	_ = counts
	rows, err := s.db.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	scores := make(map[string]float64, len(rows))
	for i, row := range rows {
		scores[row.Kind+"\x00"+row.ID] = float64(len(rows) - i)
	}
	return scores, nil
}

func (s *rhizaStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func rhizaDatabaseHasEvidence(databasePath string) bool {
	if _, err := os.Lstat(filepath.Join(databasePath, rhizaDriverMarker)); err == nil {
		return true
	}
	return rhizaNativeDatabaseHasEvidence(databasePath)
}

func rhizaNativeDatabaseHasEvidence(databasePath string) bool {
	for _, path := range []string{filepath.Join(databasePath, "qlog"), filepath.Join(databasePath, "sqlite.db"), filepath.Join(databasePath, "latticedb", "graph.ltdb")} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
