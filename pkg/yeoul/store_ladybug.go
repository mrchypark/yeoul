package yeoul

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
	json "github.com/goccy/go-json"
	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
)

type ladybugStore struct {
	cfg       Config
	store     *lstore.Store
	lastState persistedState
	loaded    bool
	failed    error
}

// mandatoryLegacyTables are the legacy node and relationship tables every
// Yeoul database must have. A database missing one is not a Yeoul database, so
// the strict migration reader refuses it instead of loading a partial snapshot.
var mandatoryLegacyTables = map[string]bool{
	"YeoulMeta":     true,
	"Source":        true,
	"Episode":       true,
	"Entity":        true,
	"Fact":          true,
	"FROM_SOURCE":   true,
	"ASSERTS":       true,
	"SUBJECT":       true,
	"OBJECT_ENTITY": true,
	"SUPPORTED_BY":  true,
	"SUPERSEDES":    true,
}

// optionalLegacyTables were added after the first schema, so an older database
// may legitimately lack them. Absent is fine; present but malformed is not.
var optionalLegacyTables = map[string]bool{
	"YeoulMigration": true,
	"EntityRevision": true,
	"FactRevision":   true,
}

func newLadybugStore(cfg Config) (stateStore, error) {
	if !cfg.ReadOnly && !cfg.legacyLadybugWrites {
		return nil, errorf(ErrNotSupported, "ladybug storage driver is read-only; migrate the database to the lattice driver to write", map[string]any{
			"driver":        string(StorageDriverLadybug),
			"database_path": cfg.DatabasePath,
		}, nil)
	}
	store, err := lstore.Open(cfg.DatabasePath, cfg.ReadOnly)
	if err != nil {
		return nil, errorf(ErrStorageFailed, "open ladybug database", map[string]any{
			"database_path": cfg.DatabasePath,
		}, err)
	}
	return &ladybugStore{
		cfg:       cfg,
		store:     store,
		lastState: emptyPersistedState(),
	}, nil
}

func (s *ladybugStore) Load() (*persistedState, error) {
	if !s.cfg.ReadOnly {
		if err := s.ensureSchema(); err != nil {
			return nil, err
		}
	}
	if s.cfg.legacyStrictRead {
		if err := s.verifyMandatoryTables(); err != nil {
			return nil, err
		}
	}

	state := emptyPersistedState()
	state.Version = 1
	if err := s.loadMeta(&state); err != nil {
		return nil, err
	}

	if err := s.loadSources(&state); err != nil {
		return nil, err
	}
	if err := s.loadEpisodes(&state); err != nil {
		return nil, err
	}
	if err := s.loadEntities(&state); err != nil {
		return nil, err
	}
	if err := s.loadFacts(&state); err != nil {
		return nil, err
	}
	if err := s.loadFactRevisions(&state); err != nil {
		return nil, err
	}
	if err := s.loadEntityRevisions(&state); err != nil {
		return nil, err
	}
	if err := s.loadMigrationWatermarks(&state); err != nil {
		return nil, err
	}
	if err := s.loadSubjectEdges(&state); err != nil {
		return nil, err
	}
	if err := s.loadObjectEdges(&state); err != nil {
		return nil, err
	}
	if err := s.loadSupportedByEdges(&state); err != nil {
		return nil, err
	}
	if err := s.loadSupersedesEdges(&state); err != nil {
		return nil, err
	}
	// FROM_SOURCE and ASSERTS carry the same information as the episode's
	// source_id column and the fact's SUPPORTED_BY edges, so the strict reader
	// compares the two representations instead of reconstructing the state from
	// only one of them. The lenient read path never consults these tables.
	if err := s.verifyFromSourceEdges(&state); err != nil {
		return nil, err
	}
	if err := s.verifyAssertsEdges(&state); err != nil {
		return nil, err
	}

	s.lastState = clonePersistedState(state)
	s.loaded = true
	return &state, nil
}

func (s *ladybugStore) Save(state persistedState) error {
	if s.cfg.ReadOnly {
		return nil
	}
	if s.failed != nil {
		return errorf(ErrStorageFailed, "ladybug legacy write path is in a failed state after a partial write; reopen the database and migrate to the lattice driver", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, s.failed)
	}
	if err := s.ensureSchema(); err != nil {
		return err
	}

	prev := s.lastState
	if !s.loaded {
		prev = emptyPersistedState()
	}

	statements := s.buildDeltaStatements(prev, state)
	if len(statements) == 0 {
		s.lastState = clonePersistedState(state)
		s.loaded = true
		return nil
	}

	if err := s.execStatements(statements); err != nil {
		s.failed = err
		return errorf(ErrStorageFailed, "write ladybug graph state; the legacy non-transactional path may have applied part of the change", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"statements":    len(statements),
		}, err)
	}
	s.lastState = clonePersistedState(state)
	s.loaded = true
	return nil
}

func (s *ladybugStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	s.store.Close()
	return nil
}

func (s *ladybugStore) ensureSchema() error {
	if err := s.execStatements(lstore.DDLStatements()); err != nil {
		return errorf(ErrStorageFailed, "ensure ladybug graph schema", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	return nil
}

// verifyMandatoryTables checks the catalog for every mandatory legacy table.
// A truncated or foreign schema can be missing a table the loaders never query
// (for example FROM_SOURCE), so the strict migration reader consults the
// catalog instead of inferring completeness from the queries it happens to run.
func (s *ladybugStore) verifyMandatoryTables() error {
	result, err := s.store.Query(lstore.QueryTables())
	if err != nil {
		return errorf(ErrStorageFailed, "read legacy database catalog", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	defer result.Close()

	present := make(map[string]bool)
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return errorf(ErrStorageFailed, "read legacy database catalog row", map[string]any{
				"database_path": s.cfg.DatabasePath,
			}, err)
		}
		values, err := tuple.GetAsSlice()
		if err != nil {
			return errorf(ErrStorageFailed, "decode legacy database catalog row", map[string]any{
				"database_path": s.cfg.DatabasePath,
			}, err)
		}
		if len(values) < 2 {
			continue
		}
		present[asString(values[1])] = true
	}

	missing := make([]string, 0, len(mandatoryLegacyTables))
	for table := range mandatoryLegacyTables {
		if !present[table] {
			missing = append(missing, table)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return errorf(ErrStorageFailed, "legacy database is missing a mandatory table", map[string]any{
		"database_path": s.cfg.DatabasePath,
		"tables":        missing,
	}, nil)
}

// legacyFields decodes the properties of one legacy node. The ordinary read
// path keeps its historical coercions; the strict migration reader records the
// first malformed or unexpectedly typed field instead, so corruption cannot be
// dropped on the floor before the cutover. The first error wins and the caller
// checks it once the whole record is decoded.
type legacyFields struct {
	store  *ladybugStore
	table  string
	record string
	props  map[string]any
	err    error
}

func (s *ladybugStore) legacyFields(table string, node lbug.Node) *legacyFields {
	return &legacyFields{
		store:  s,
		table:  table,
		record: asString(node.Properties["id"]),
		props:  node.Properties,
	}
}

func (f *legacyFields) result() error { return f.err }

func (f *legacyFields) fail(err error) {
	if f.err == nil {
		f.err = err
	}
}

func (f *legacyFields) string(field string) string {
	value := f.props[field]
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if ok {
		return text
	}
	if f.store.cfg.legacyStrictRead {
		f.fail(f.unexpectedType(field, value))
	}
	return fmt.Sprint(value)
}

func (f *legacyFields) metadata(field string) map[string]any {
	decoded, err := decodeJSONMapStrict(f.string(field))
	if err != nil && f.store.cfg.legacyStrictRead {
		f.fail(f.malformed(field, err))
	}
	return decoded
}

func (f *legacyFields) stringSlice(field string) []string {
	decoded, err := decodeJSONStringSliceStrict(f.string(field))
	if err != nil && f.store.cfg.legacyStrictRead {
		f.fail(f.malformed(field, err))
	}
	return decoded
}

func (f *legacyFields) float(field string) float64 {
	switch value := f.props[field].(type) {
	case nil:
		return 0
	case float64:
		return value
	case float32:
		return float64(value)
	case int64:
		return float64(value)
	case int:
		return float64(value)
	default:
		if f.store.cfg.legacyStrictRead {
			f.fail(f.unexpectedType(field, value))
		}
		return 0
	}
}

func (f *legacyFields) timestamp(field string) time.Time {
	value := f.props[field]
	switch typed := value.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return typed.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			if f.store.cfg.legacyStrictRead {
				f.fail(f.malformed(field, err))
			}
			return time.Time{}
		}
		return parsed.UTC()
	default:
		if f.store.cfg.legacyStrictRead {
			f.fail(f.unexpectedType(field, value))
		}
		return time.Time{}
	}
}

func (f *legacyFields) unexpectedType(field string, value any) error {
	return errorf(ErrStorageFailed, "legacy record has an unexpectedly typed field", map[string]any{
		"database_path": f.store.cfg.DatabasePath,
		"table":         f.table,
		"record_id":     f.record,
		"field":         field,
		"value_type":    fmt.Sprintf("%T", value),
	}, nil)
}

func (f *legacyFields) malformed(field string, cause error) error {
	return errorf(ErrStorageFailed, "legacy record has a malformed field", map[string]any{
		"database_path": f.store.cfg.DatabasePath,
		"table":         f.table,
		"record_id":     f.record,
		"field":         field,
	}, cause)
}

func (s *ladybugStore) loadSources(state *persistedState) error {
	return s.loadNodes("Source", func(node lbug.Node) error {
		fields := s.legacyFields("Source", node)
		source := Source{
			ID:          fields.string("id"),
			SpaceID:     fields.string("space_id"),
			Kind:        fields.string("kind"),
			URI:         fields.string("uri"),
			ExternalRef: fields.string("external_ref"),
			Metadata:    fields.metadata("metadata_json"),
			CreatedAt:   fields.timestamp("created_at"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.Sources[source.ID] = source
		return nil
	})
}

func (s *ladybugStore) loadMeta(state *persistedState) error {
	// The legacy writer only creates the singleton meta row once it has to
	// generate an id, so a database whose records all carry explicit ids has no
	// row at all. An absent row is therefore legal and means sequence 0; a
	// present row must still decode strictly.
	return s.loadRows("YeoulMeta", lstore.QueryMetaSequence(), func(values []any) error {
		if err := s.checkMetaRow(values); err != nil {
			return err
		}
		state.Sequence = asUint64(values[0])
		return nil
	})
}

// checkMetaRow validates the singleton meta row in strict mode. The meta row
// carries live state (the id sequence), so a missing column or a sequence that
// is not a non-negative integer must fail the migration instead of being
// coerced to zero.
func (s *ladybugStore) checkMetaRow(values []any) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	if len(values) == 0 {
		return errorf(ErrStorageFailed, "legacy meta row is missing its sequence column", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"table":         "YeoulMeta",
		}, nil)
	}
	switch value := values[0].(type) {
	case int64:
		if value < 0 {
			return s.malformedMetaSequence(value)
		}
	case int:
		if value < 0 {
			return s.malformedMetaSequence(value)
		}
	case uint64:
	default:
		return s.malformedMetaSequence(values[0])
	}
	return nil
}

func (s *ladybugStore) malformedMetaSequence(value any) error {
	return errorf(ErrStorageFailed, "legacy meta row has a malformed sequence value", map[string]any{
		"database_path": s.cfg.DatabasePath,
		"table":         "YeoulMeta",
		"field":         "sequence",
		"value_type":    fmt.Sprintf("%T", value),
	}, nil)
}

func (s *ladybugStore) loadEpisodes(state *persistedState) error {
	return s.loadNodes("Episode", func(node lbug.Node) error {
		fields := s.legacyFields("Episode", node)
		episode := Episode{
			ID:         fields.string("id"),
			SpaceID:    fields.string("space_id"),
			Kind:       fields.string("kind"),
			Content:    fields.string("content"),
			SourceID:   fields.string("source_id"),
			GroupID:    fields.string("group_id"),
			ObservedAt: fields.timestamp("observed_at"),
			IngestedAt: fields.timestamp("ingested_at"),
			Metadata:   fields.metadata("metadata_json"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.Episodes[episode.ID] = episode
		return nil
	})
}

func (s *ladybugStore) loadEntities(state *persistedState) error {
	return s.loadNodes("Entity", func(node lbug.Node) error {
		fields := s.legacyFields("Entity", node)
		entity := Entity{
			ID:            fields.string("id"),
			SpaceID:       fields.string("space_id"),
			Namespace:     fields.string("namespace"),
			Type:          fields.string("type"),
			CanonicalName: fields.string("canonical_name"),
			Aliases:       fields.stringSlice("aliases_json"),
			Metadata:      fields.metadata("metadata_json"),
			CreatedAt:     fields.timestamp("created_at"),
			UpdatedAt:     fields.timestamp("updated_at"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.Entities[entity.ID] = entity
		return nil
	})
}

func (s *ladybugStore) loadFacts(state *persistedState) error {
	return s.loadNodes("Fact", func(node lbug.Node) error {
		fields := s.legacyFields("Fact", node)
		fact := Fact{
			ID:               fields.string("id"),
			SpaceID:          fields.string("space_id"),
			Predicate:        fields.string("predicate"),
			ValueText:        fields.string("value_text"),
			Confidence:       fields.float("confidence"),
			Status:           fields.string("status"),
			ValidFrom:        fields.timestamp("valid_from"),
			ValidTo:          fields.timestamp("valid_to"),
			ObservedAt:       fields.timestamp("observed_at"),
			CreatedAt:        fields.timestamp("created_at"),
			UpdatedAt:        fields.timestamp("updated_at"),
			RetractedAt:      fields.timestamp("retracted_at"),
			RetractionReason: fields.string("retraction_reason"),
			Metadata:         fields.metadata("metadata_json"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.Facts[fact.ID] = fact
		return nil
	})
}

func (s *ladybugStore) loadFactRevisions(state *persistedState) error {
	return s.loadNodes("FactRevision", func(node lbug.Node) error {
		fields := s.legacyFields("FactRevision", node)
		revision := FactRevision{
			ID:                   fields.string("id"),
			FactID:               fields.string("fact_id"),
			SpaceID:              fields.string("space_id"),
			RevisionKind:         fields.string("revision_kind"),
			TxTime:               fields.timestamp("tx_time"),
			Predicate:            fields.string("predicate"),
			SubjectID:            fields.string("subject_id"),
			ObjectID:             fields.string("object_id"),
			ValueText:            fields.string("value_text"),
			Confidence:           fields.float("confidence"),
			Status:               fields.string("status"),
			ValidFrom:            fields.timestamp("valid_from"),
			ValidTo:              fields.timestamp("valid_to"),
			ObservedAt:           fields.timestamp("observed_at"),
			CreatedAt:            fields.timestamp("created_at"),
			UpdatedAt:            fields.timestamp("updated_at"),
			RetractedAt:          fields.timestamp("retracted_at"),
			RetractionReason:     fields.string("retraction_reason"),
			SupportingEpisodeIDs: fields.stringSlice("supporting_episode_ids_json"),
			Metadata:             fields.metadata("metadata_json"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.FactRevisions[revision.ID] = revision
		return nil
	})
}

func (s *ladybugStore) loadEntityRevisions(state *persistedState) error {
	return s.loadNodes("EntityRevision", func(node lbug.Node) error {
		fields := s.legacyFields("EntityRevision", node)
		revision := EntityRevision{
			ID:            fields.string("id"),
			EntityID:      fields.string("entity_id"),
			SpaceID:       fields.string("space_id"),
			RevisionKind:  fields.string("revision_kind"),
			TxTime:        fields.timestamp("tx_time"),
			Namespace:     fields.string("namespace"),
			Type:          fields.string("type"),
			CanonicalName: fields.string("canonical_name"),
			Aliases:       fields.stringSlice("aliases_json"),
			Metadata:      fields.metadata("metadata_json"),
			CreatedAt:     fields.timestamp("created_at"),
			UpdatedAt:     fields.timestamp("updated_at"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.EntityRevisions[revision.ID] = revision
		return nil
	})
}

func (s *ladybugStore) loadMigrationWatermarks(state *persistedState) error {
	return s.loadNodes("YeoulMigration", func(node lbug.Node) error {
		fields := s.legacyFields("YeoulMigration", node)
		watermark := MigrationWatermark{
			ID:        fields.string("id"),
			AppliedAt: fields.timestamp("applied_at"),
			Metadata:  fields.metadata("metadata_json"),
		}
		if err := fields.result(); err != nil {
			return err
		}
		state.MigrationWatermarks[watermark.ID] = watermark
		return nil
	})
}

func (s *ladybugStore) loadSubjectEdges(state *persistedState) error {
	return s.loadRows("SUBJECT", lstore.QuerySubjectEdges(), func(values []any) error {
		if err := s.checkEdgeRow("SUBJECT", values, 2, 2); err != nil {
			return err
		}
		factID, entityID := asString(values[0]), asString(values[1])
		if err := s.checkFactEdgeReference("SUBJECT", factID, "Fact", state.Facts); err != nil {
			return err
		}
		if err := s.checkEntityEdgeReference("SUBJECT", entityID, state.Entities); err != nil {
			return err
		}
		fact := state.Facts[factID]
		fact.SubjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadObjectEdges(state *persistedState) error {
	return s.loadRows("OBJECT_ENTITY", lstore.QueryObjectEdges(), func(values []any) error {
		if err := s.checkEdgeRow("OBJECT_ENTITY", values, 2, 2); err != nil {
			return err
		}
		factID, entityID := asString(values[0]), asString(values[1])
		if err := s.checkFactEdgeReference("OBJECT_ENTITY", factID, "Fact", state.Facts); err != nil {
			return err
		}
		if err := s.checkEntityEdgeReference("OBJECT_ENTITY", entityID, state.Entities); err != nil {
			return err
		}
		fact := state.Facts[factID]
		fact.ObjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadSupportedByEdges(state *persistedState) error {
	// Strict migration reads must not collapse duplicate SUPPORTED_BY rows: the
	// writer creates each edge at most once, so a repeated (fact, episode) row is
	// corruption that must fail instead of being normalized away before the
	// ASSERTS agreement check runs. Lenient reads keep deduplicating.
	var seen map[string]map[string]bool
	if s.cfg.legacyStrictRead {
		seen = make(map[string]map[string]bool, len(state.Facts))
	}
	return s.loadRows("SUPPORTED_BY", lstore.QuerySupportedByEdges(), func(values []any) error {
		if err := s.checkEdgeRow("SUPPORTED_BY", values, 2, 2); err != nil {
			return err
		}
		factID, episodeID := asString(values[0]), asString(values[1])
		if err := s.checkFactEdgeReference("SUPPORTED_BY", factID, "Fact", state.Facts); err != nil {
			return err
		}
		if _, ok := state.Episodes[episodeID]; !ok && s.cfg.legacyStrictRead {
			return s.danglingReference("SUPPORTED_BY", "Episode", episodeID)
		}
		if s.cfg.legacyStrictRead {
			if seen[factID] == nil {
				seen[factID] = make(map[string]bool, 1)
			}
			if seen[factID][episodeID] {
				return s.duplicateEdge("SUPPORTED_BY", "fact", factID, episodeID)
			}
			seen[factID][episodeID] = true
		}
		fact := state.Facts[factID]
		if !slices.Contains(fact.SupportingEpisodeIDs, episodeID) {
			fact.SupportingEpisodeIDs = append(fact.SupportingEpisodeIDs, episodeID)
		}
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadSupersedesEdges(state *persistedState) error {
	return s.loadRows("SUPERSEDES", lstore.QuerySupersedesEdges(), func(values []any) error {
		if err := s.checkEdgeRow("SUPERSEDES", values, 3, 2); err != nil {
			return err
		}
		if err := s.checkSupersedesReason(values); err != nil {
			return err
		}
		newID, oldID, reason := asString(values[0]), asString(values[1]), asString(values[2])
		if err := s.checkFactEdgeReference("SUPERSEDES", newID, "Fact", state.Facts); err != nil {
			return err
		}
		if err := s.checkFactEdgeReference("SUPERSEDES", oldID, "Fact", state.Facts); err != nil {
			return err
		}
		oldFact := state.Facts[oldID]
		oldFact.Metadata = mergeAnyMap(oldFact.Metadata, map[string]any{
			"superseded_by":    newID,
			"supersede_reason": reason,
		})
		state.Facts[oldID] = oldFact
		newFact := state.Facts[newID]
		supersedes := metadataStringIDs(newFact.Metadata["supersedes"])
		supersedes = append(supersedes, oldID)
		newFact.Metadata = mergeAnyMap(newFact.Metadata, map[string]any{
			"supersedes":       dedupeStrings(supersedes),
			"supersede_reason": reason,
		})
		state.Facts[newID] = newFact
		return nil
	})
}

// verifyFromSourceEdges compares the mandatory FROM_SOURCE relationship table
// against the source_id the episode loader decoded. The writer emits exactly one
// edge per episode whose source_id is non-empty and no edge for an episode whose
// source_id is empty, so in strict mode a missing, extra, retargeted, or
// duplicate edge is a disagreement between two persisted representations of the
// same state and must fail the migration instead of letting the reader pick one.
func (s *ladybugStore) verifyFromSourceEdges(state *persistedState) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	targets := make(map[string][]string, len(state.Episodes))
	err := s.loadRows("FROM_SOURCE", lstore.QueryFromSourceEdges(), func(values []any) error {
		if err := s.checkEdgeRow("FROM_SOURCE", values, 2, 2); err != nil {
			return err
		}
		episodeID, sourceID := asString(values[0]), asString(values[1])
		if _, ok := state.Episodes[episodeID]; !ok {
			return s.danglingReference("FROM_SOURCE", "Episode", episodeID)
		}
		if _, ok := state.Sources[sourceID]; !ok {
			return s.danglingReference("FROM_SOURCE", "Source", sourceID)
		}
		targets[episodeID] = append(targets[episodeID], sourceID)
		return nil
	})
	if err != nil {
		return err
	}
	for _, episodeID := range sortedKeys(state.Episodes) {
		episode := state.Episodes[episodeID]
		expected := 0
		if episode.SourceID != "" {
			expected = 1
		}
		actual := targets[episodeID]
		if len(actual) != expected {
			return s.relationshipDisagreement("FROM_SOURCE", "episode", episodeID, episode.SourceID, actual)
		}
		if expected == 1 && actual[0] != episode.SourceID {
			return s.relationshipDisagreement("FROM_SOURCE", "episode", episodeID, episode.SourceID, actual)
		}
	}
	return nil
}

// verifyAssertsEdges compares the mandatory ASSERTS relationship table against
// the supporting episodes the fact loader decoded. The writer emits exactly one
// ASSERTS edge for every (episode, fact) pair in the fact's supporting set and
// nothing for a fact without support, so strict mode requires the two
// representations to agree exactly, including multiplicity.
func (s *ladybugStore) verifyAssertsEdges(state *persistedState) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	asserted := make(map[string]map[string]bool, len(state.Facts))
	err := s.loadRows("ASSERTS", lstore.QueryAssertsEdges(), func(values []any) error {
		if err := s.checkEdgeRow("ASSERTS", values, 2, 2); err != nil {
			return err
		}
		episodeID, factID := asString(values[0]), asString(values[1])
		if _, ok := state.Episodes[episodeID]; !ok {
			return s.danglingReference("ASSERTS", "Episode", episodeID)
		}
		if _, ok := state.Facts[factID]; !ok {
			return s.danglingReference("ASSERTS", "Fact", factID)
		}
		if asserted[factID] == nil {
			asserted[factID] = make(map[string]bool)
		}
		if asserted[factID][episodeID] {
			return s.duplicateEdge("ASSERTS", "fact", factID, episodeID)
		}
		asserted[factID][episodeID] = true
		return nil
	})
	if err != nil {
		return err
	}
	for _, factID := range sortedKeys(state.Facts) {
		fact := state.Facts[factID]
		expected := make(map[string]bool, len(fact.SupportingEpisodeIDs))
		for _, episodeID := range fact.SupportingEpisodeIDs {
			expected[episodeID] = true
		}
		actual := asserted[factID]
		if len(actual) != len(expected) {
			return s.relationshipDisagreement("ASSERTS", "fact", factID, "", sortedKeys(actual))
		}
		for episodeID := range expected {
			if !actual[episodeID] {
				return s.relationshipDisagreement("ASSERTS", "fact", factID, "", sortedKeys(actual))
			}
		}
	}
	return nil
}

// relationshipDisagreement reports that a mandatory relationship table does not
// match the state decoded from the node columns it duplicates. Neither
// representation is preferred, so the migration fails and names the relation,
// the offending record, and both sides of the disagreement.
func (s *ladybugStore) relationshipDisagreement(table, kind, recordID, expected string, actual []string) error {
	details := map[string]any{
		"database_path": s.cfg.DatabasePath,
		"table":         table,
		"record_kind":   kind,
		"record_id":     recordID,
		"edges":         actual,
	}
	if expected != "" {
		details["expected"] = expected
	}
	return errorf(ErrStorageFailed, "legacy relationship rows disagree with the decoded legacy state", details, nil)
}

// duplicateEdge reports a relationship table that stores the same edge more
// than once. The writer creates each edge at most once, and the lenient loader
// collapses duplicates, so a repeat row is corruption rather than a legal
// legacy shape.
func (s *ladybugStore) duplicateEdge(table, kind, recordID, duplicateID string) error {
	return errorf(ErrStorageFailed, "legacy relationship table stores a duplicate edge", map[string]any{
		"database_path": s.cfg.DatabasePath,
		"table":         table,
		"record_kind":   kind,
		"record_id":     recordID,
		"duplicate_id":  duplicateID,
	}, nil)
}

func (s *ladybugStore) loadNodes(label string, apply func(node lbug.Node) error) error {
	return s.loadRows(label, lstore.QueryNodes(label), func(values []any) error {
		if len(values) == 0 {
			if s.cfg.legacyStrictRead {
				return errorf(ErrStorageFailed, "read legacy node row without a value column", map[string]any{
					"database_path": s.cfg.DatabasePath,
					"table":         label,
				}, nil)
			}
			return nil
		}
		node, ok := values[0].(lbug.Node)
		if !ok {
			if s.cfg.legacyStrictRead {
				return errorf(ErrStorageFailed, "read legacy node row with an unexpected value type", map[string]any{
					"database_path": s.cfg.DatabasePath,
					"table":         label,
					"value_type":    fmt.Sprintf("%T", values[0]),
				}, nil)
			}
			return nil
		}
		return apply(node)
	})
}

// loadRows runs one legacy read query. In strict mode a missing mandatory table
// is a hard failure, while the optional tables added after the first schema may
// still be absent. The lenient read path keeps its previous behavior.
func (s *ladybugStore) loadRows(table, query string, apply func(values []any) error) error {
	result, err := s.store.Query(query)
	if err != nil {
		if s.cfg.ReadOnly && lstore.IsMissingTableError(err) {
			if !s.cfg.legacyStrictRead || optionalLegacyTables[table] {
				return nil
			}
			return errorf(ErrStorageFailed, "legacy database is missing a mandatory table", map[string]any{
				"database_path": s.cfg.DatabasePath,
				"table":         table,
			}, err)
		}
		return errorf(ErrStorageFailed, "query ladybug graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"query":         query,
		}, err)
	}
	defer result.Close()

	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return errorf(ErrStorageFailed, "read ladybug graph row", map[string]any{
				"database_path": s.cfg.DatabasePath,
				"query":         query,
			}, err)
		}
		values, err := tuple.GetAsSlice()
		if err != nil {
			return errorf(ErrStorageFailed, "decode ladybug graph row", map[string]any{
				"database_path": s.cfg.DatabasePath,
				"query":         query,
			}, err)
		}
		if err := apply(values); err != nil {
			return err
		}
	}
	return nil
}

// checkSupersedesReason validates the trailing reason column of a SUPERSEDES
// row. The column is nullable, so a string or SQL NULL is legal and every
// other representation must fail instead of being stringified into a value
// that differs from what the source database stored.
func (s *ladybugStore) checkSupersedesReason(values []any) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	if _, ok := values[2].(string); !ok && values[2] != nil {
		return errorf(ErrStorageFailed, "legacy relationship row has an unexpectedly typed reason", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"table":         "SUPERSEDES",
			"column":        "reason",
			"value_type":    fmt.Sprintf("%T", values[2]),
		}, nil)
	}
	return nil
}

// checkEdgeRow rejects a relationship row whose projected columns are missing
// or whose id columns are not strings. A row that does not carry the ids its
// query projects cannot be resolved to records, so the strict reader refuses it
// instead of writing an edge onto the empty id. Columns past idColumns are
// validated by the caller when they carry meaning.
func (s *ladybugStore) checkEdgeRow(table string, values []any, total, idColumns int) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	if len(values) < total {
		return errorf(ErrStorageFailed, "legacy relationship row is missing projected columns", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"table":         table,
			"columns":       len(values),
			"expected":      total,
		}, nil)
	}
	for index := 0; index < idColumns; index++ {
		if _, ok := values[index].(string); !ok {
			return errorf(ErrStorageFailed, "legacy relationship row has an unexpectedly typed column", map[string]any{
				"database_path": s.cfg.DatabasePath,
				"table":         table,
				"column":        index,
				"value_type":    fmt.Sprintf("%T", values[index]),
			}, nil)
		}
	}
	return nil
}

func (s *ladybugStore) checkFactEdgeReference(table, factID, label string, facts map[string]Fact) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	if _, ok := facts[factID]; ok {
		return nil
	}
	return s.danglingReference(table, label, factID)
}

func (s *ladybugStore) checkEntityEdgeReference(table, entityID string, entities map[string]Entity) error {
	if !s.cfg.legacyStrictRead {
		return nil
	}
	if _, ok := entities[entityID]; ok {
		return nil
	}
	return s.danglingReference(table, "Entity", entityID)
}

func (s *ladybugStore) danglingReference(table, label, id string) error {
	return errorf(ErrStorageFailed, "legacy relationship references a missing record", map[string]any{
		"database_path":  s.cfg.DatabasePath,
		"table":          table,
		"missing_label":  label,
		"missing_record": id,
	}, nil)
}

// execStatements runs legacy write statements one by one so a later failure is
// surfaced instead of being hidden behind the first statement's result.
func (s *ladybugStore) execStatements(statements []string) error {
	return s.store.ExecuteStatements(statements)
}

func (s *ladybugStore) buildDeltaStatements(prev, next persistedState) []string {
	statements := make([]string, 0, 128)

	statements = append(statements, s.buildMetaDelta(prev, next)...)
	statements = append(statements, s.buildDeletedFactStatements(prev.Facts, next.Facts)...)
	statements = append(statements, s.buildDeletedEpisodeStatements(prev.Episodes, next.Episodes)...)
	statements = append(statements, s.buildDeletedEntityStatements(prev.Entities, next.Entities)...)
	statements = append(statements, s.buildDeletedSourceStatements(prev.Sources, next.Sources)...)

	statements = append(statements, s.buildSourceDelta(prev.Sources, next.Sources)...)
	statements = append(statements, s.buildEpisodeDelta(prev.Episodes, next.Episodes)...)
	statements = append(statements, s.buildEntityDelta(prev.Entities, next.Entities)...)
	statements = append(statements, s.buildFactDelta(prev.Facts, next.Facts)...)
	statements = append(statements, s.buildMigrationWatermarkDelta(prev.MigrationWatermarks, next.MigrationWatermarks)...)
	statements = append(statements, s.buildEntityRevisionDelta(prev.EntityRevisions, next.EntityRevisions)...)
	statements = append(statements, s.buildFactRevisionDelta(prev.FactRevisions, next.FactRevisions)...)

	statements = append(statements, s.buildEpisodeRelationshipDelta(prev.Episodes, next.Episodes)...)
	statements = append(statements, s.buildFactRelationshipDelta(prev.Facts, next.Facts)...)
	return statements
}

func (s *ladybugStore) buildMetaDelta(prev, next persistedState) []string {
	if prev.Sequence == next.Sequence && s.loaded {
		return nil
	}
	return []string{
		lstore.DeleteMetaSequence(),
		lstore.CreateMetaSequence(next.Sequence),
	}
}

func (s *ladybugStore) buildSourceDelta(prev, next map[string]Source) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldSource, oldOK := prev[id]
		newSource, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, lstore.CreateNode(sourceRecord(newSource)))
		case oldOK && newOK && !reflect.DeepEqual(oldSource, newSource):
			if stmt, ok := lstore.UpdateNode(sourceRecord(newSource)); ok {
				statements = append(statements, stmt)
			}
		}
	}
	return statements
}

func (s *ladybugStore) buildEpisodeDelta(prev, next map[string]Episode) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldEpisode, oldOK := prev[id]
		newEpisode, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, lstore.CreateNode(episodeRecord(newEpisode)))
		case oldOK && newOK && !reflect.DeepEqual(oldEpisode, newEpisode):
			if stmt, ok := lstore.UpdateNode(episodeRecord(newEpisode)); ok {
				statements = append(statements, stmt)
			}
		}
	}
	return statements
}

func (s *ladybugStore) buildEntityDelta(prev, next map[string]Entity) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldEntity, oldOK := prev[id]
		newEntity, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, lstore.CreateNode(entityRecord(newEntity)))
		case oldOK && newOK && !reflect.DeepEqual(oldEntity, newEntity):
			if stmt, ok := lstore.UpdateNode(entityRecord(newEntity)); ok {
				statements = append(statements, stmt)
			}
		}
	}
	return statements
}

func (s *ladybugStore) buildFactDelta(prev, next map[string]Fact) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldFact, oldOK := prev[id]
		newFact, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, lstore.CreateNode(factRecord(newFact)))
		case oldOK && newOK && !reflect.DeepEqual(stripFactRelationshipFields(oldFact), stripFactRelationshipFields(newFact)):
			if stmt, ok := lstore.UpdateNode(factRecord(newFact)); ok {
				statements = append(statements, stmt)
			}
		}
	}
	return statements
}

func (s *ladybugStore) buildMigrationWatermarkDelta(prev, next map[string]MigrationWatermark) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldWatermark, oldOK := prev[id]
		newWatermark, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, lstore.CreateNode(watermarkRecord(newWatermark)))
		case oldOK && newOK && !reflect.DeepEqual(oldWatermark, newWatermark):
			if stmt, ok := lstore.UpdateNode(watermarkRecord(newWatermark)); ok {
				statements = append(statements, stmt)
			}
		}
	}
	return statements
}

func (s *ladybugStore) buildEntityRevisionDelta(prev, next map[string]EntityRevision) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(next) {
		if _, ok := prev[id]; ok {
			continue
		}
		statements = append(statements, lstore.CreateNode(entityRevisionRecord(next[id])))
	}
	return statements
}

func (s *ladybugStore) buildFactRevisionDelta(prev, next map[string]FactRevision) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(next) {
		if _, ok := prev[id]; ok {
			continue
		}
		statements = append(statements, lstore.CreateNode(factRevisionRecord(next[id])))
	}
	return statements
}

func (s *ladybugStore) buildEpisodeRelationshipDelta(prev, next map[string]Episode) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldEpisode, oldOK := prev[id]
		newEpisode, newOK := next[id]
		switch {
		case oldOK && !newOK:
			statements = append(statements, deleteEpisodeRelationshipStatements(oldEpisode)...)
		case !oldOK && newOK:
			statements = append(statements, createEpisodeRelationshipStatements(newEpisode)...)
		case oldOK && newOK && episodeRelationshipsChanged(oldEpisode, newEpisode):
			statements = append(statements, deleteEpisodeRelationshipStatements(oldEpisode)...)
			statements = append(statements, createEpisodeRelationshipStatements(newEpisode)...)
		}
	}
	return statements
}

func (s *ladybugStore) buildFactRelationshipDelta(prev, next map[string]Fact) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldFact, oldOK := prev[id]
		newFact, newOK := next[id]
		switch {
		case oldOK && !newOK:
			statements = append(statements, deleteFactRelationshipStatements(oldFact)...)
		case !oldOK && newOK:
			statements = append(statements, createFactRelationshipStatements(newFact)...)
		case oldOK && newOK && factRelationshipsChanged(oldFact, newFact):
			statements = append(statements, deleteFactRelationshipStatements(oldFact)...)
			statements = append(statements, createFactRelationshipStatements(newFact)...)
		}
	}
	return statements
}

func (s *ladybugStore) buildDeletedFactStatements(prev, next map[string]Fact) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, lstore.DeleteNode("Fact", id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedEpisodeStatements(prev, next map[string]Episode) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, lstore.DeleteNode("Episode", id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedEntityStatements(prev, next map[string]Entity) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, lstore.DeleteNode("Entity", id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedSourceStatements(prev, next map[string]Source) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, lstore.DeleteNode("Source", id))
	}
	return statements
}

func sourceRecord(source Source) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "Source",
		ID:    source.ID,
		Props: map[string]string{
			"space_id":      lstore.StringLiteral(source.SpaceID),
			"kind":          lstore.StringLiteral(source.Kind),
			"uri":           lstore.StringLiteral(source.URI),
			"external_ref":  lstore.StringLiteral(source.ExternalRef),
			"created_at":    lstore.TimeLiteral(source.CreatedAt),
			"metadata_json": lstore.JSONLiteral(source.Metadata),
		},
	}
}

func episodeRecord(episode Episode) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "Episode",
		ID:    episode.ID,
		Props: map[string]string{
			"space_id":      lstore.StringLiteral(episode.SpaceID),
			"kind":          lstore.StringLiteral(episode.Kind),
			"content":       lstore.StringLiteral(episode.Content),
			"content_hash":  lstore.StringLiteral(""),
			"source_id":     lstore.StringLiteral(episode.SourceID),
			"group_id":      lstore.StringLiteral(episode.GroupID),
			"observed_at":   lstore.TimeLiteral(episode.ObservedAt),
			"ingested_at":   lstore.TimeLiteral(episode.IngestedAt),
			"metadata_json": lstore.JSONLiteral(episode.Metadata),
		},
	}
}

func entityRecord(entity Entity) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "Entity",
		ID:    entity.ID,
		Props: map[string]string{
			"space_id":       lstore.StringLiteral(entity.SpaceID),
			"namespace":      lstore.StringLiteral(entity.Namespace),
			"type":           lstore.StringLiteral(entity.Type),
			"canonical_name": lstore.StringLiteral(entity.CanonicalName),
			"aliases_json":   lstore.JSONLiteral(entity.Aliases),
			"fingerprint":    lstore.StringLiteral(""),
			"created_at":     lstore.TimeLiteral(entity.CreatedAt),
			"updated_at":     lstore.TimeLiteral(entity.UpdatedAt),
			"metadata_json":  lstore.JSONLiteral(entity.Metadata),
		},
	}
}

func factRecord(fact Fact) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "Fact",
		ID:    fact.ID,
		Props: map[string]string{
			"space_id":          lstore.StringLiteral(fact.SpaceID),
			"predicate":         lstore.StringLiteral(fact.Predicate),
			"value_text":        lstore.StringLiteral(fact.ValueText),
			"confidence":        lstore.FloatLiteral(fact.Confidence),
			"status":            lstore.StringLiteral(fact.Status),
			"valid_from":        lstore.TimeLiteral(fact.ValidFrom),
			"valid_to":          lstore.TimeLiteral(fact.ValidTo),
			"observed_at":       lstore.TimeLiteral(fact.ObservedAt),
			"created_at":        lstore.TimeLiteral(fact.CreatedAt),
			"updated_at":        lstore.TimeLiteral(fact.UpdatedAt),
			"retracted_at":      lstore.TimeLiteral(fact.RetractedAt),
			"retraction_reason": lstore.StringLiteral(fact.RetractionReason),
			"metadata_json":     lstore.JSONLiteral(fact.Metadata),
		},
	}
}

func watermarkRecord(watermark MigrationWatermark) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "YeoulMigration",
		ID:    watermark.ID,
		Props: map[string]string{
			"applied_at":    lstore.TimeLiteral(watermark.AppliedAt),
			"metadata_json": lstore.JSONLiteral(watermark.Metadata),
		},
	}
}

func entityRevisionRecord(revision EntityRevision) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "EntityRevision",
		ID:    revision.ID,
		Props: map[string]string{
			"entity_id":      lstore.StringLiteral(revision.EntityID),
			"space_id":       lstore.StringLiteral(revision.SpaceID),
			"revision_kind":  lstore.StringLiteral(revision.RevisionKind),
			"tx_time":        lstore.TimeLiteral(revision.TxTime),
			"namespace":      lstore.StringLiteral(revision.Namespace),
			"type":           lstore.StringLiteral(revision.Type),
			"canonical_name": lstore.StringLiteral(revision.CanonicalName),
			"aliases_json":   lstore.JSONLiteral(revision.Aliases),
			"created_at":     lstore.TimeLiteral(revision.CreatedAt),
			"updated_at":     lstore.TimeLiteral(revision.UpdatedAt),
			"metadata_json":  lstore.JSONLiteral(revision.Metadata),
		},
	}
}

func factRevisionRecord(revision FactRevision) lstore.NodeRecord {
	return lstore.NodeRecord{
		Label: "FactRevision",
		ID:    revision.ID,
		Props: map[string]string{
			"fact_id":                     lstore.StringLiteral(revision.FactID),
			"space_id":                    lstore.StringLiteral(revision.SpaceID),
			"revision_kind":               lstore.StringLiteral(revision.RevisionKind),
			"tx_time":                     lstore.TimeLiteral(revision.TxTime),
			"predicate":                   lstore.StringLiteral(revision.Predicate),
			"subject_id":                  lstore.StringLiteral(revision.SubjectID),
			"object_id":                   lstore.StringLiteral(revision.ObjectID),
			"value_text":                  lstore.StringLiteral(revision.ValueText),
			"confidence":                  lstore.FloatLiteral(revision.Confidence),
			"status":                      lstore.StringLiteral(revision.Status),
			"valid_from":                  lstore.TimeLiteral(revision.ValidFrom),
			"valid_to":                    lstore.TimeLiteral(revision.ValidTo),
			"observed_at":                 lstore.TimeLiteral(revision.ObservedAt),
			"created_at":                  lstore.TimeLiteral(revision.CreatedAt),
			"updated_at":                  lstore.TimeLiteral(revision.UpdatedAt),
			"retracted_at":                lstore.TimeLiteral(revision.RetractedAt),
			"retraction_reason":           lstore.StringLiteral(revision.RetractionReason),
			"supporting_episode_ids_json": lstore.JSONLiteral(revision.SupportingEpisodeIDs),
			"metadata_json":               lstore.JSONLiteral(revision.Metadata),
		},
	}
}

func createEpisodeRelationshipStatements(episode Episode) []string {
	if episode.SourceID == "" {
		return nil
	}
	return []string{
		lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Episode",
			FromID:    episode.ID,
			Type:      "FROM_SOURCE",
			ToLabel:   "Source",
			ToID:      episode.SourceID,
			Props: map[string]string{
				"created_at": lstore.TimeLiteral(episode.IngestedAt),
			},
		}),
	}
}

func deleteEpisodeRelationshipStatements(episode Episode) []string {
	return []string{
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Episode", FromID: episode.ID, Type: "FROM_SOURCE", ToLabel: "Source"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Episode", FromID: episode.ID, Type: "ASSERTS", ToLabel: "Fact"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", Type: "SUPPORTED_BY", ToLabel: "Episode", ToID: episode.ID}),
	}
}

func createFactRelationshipStatements(fact Fact) []string {
	statements := make([]string, 0, 6+len(fact.SupportingEpisodeIDs)*2)
	if fact.SubjectID != "" {
		statements = append(statements, lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Fact",
			FromID:    fact.ID,
			Type:      "SUBJECT",
			ToLabel:   "Entity",
			ToID:      fact.SubjectID,
			Props: map[string]string{
				"created_at": lstore.TimeLiteral(fact.CreatedAt),
			},
		}))
	}
	if fact.ObjectID != "" {
		statements = append(statements, lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Fact",
			FromID:    fact.ID,
			Type:      "OBJECT_ENTITY",
			ToLabel:   "Entity",
			ToID:      fact.ObjectID,
			Props: map[string]string{
				"created_at": lstore.TimeLiteral(fact.CreatedAt),
			},
		}))
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		statements = append(statements, lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Episode",
			FromID:    episodeID,
			Type:      "ASSERTS",
			ToLabel:   "Fact",
			ToID:      fact.ID,
			Props: map[string]string{
				"created_at": lstore.TimeLiteral(fact.CreatedAt),
			},
		}))
		statements = append(statements, lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Fact",
			FromID:    fact.ID,
			Type:      "SUPPORTED_BY",
			ToLabel:   "Episode",
			ToID:      episodeID,
			Props: map[string]string{
				"support_kind": lstore.StringLiteral("observed"),
				"created_at":   lstore.TimeLiteral(fact.CreatedAt),
			},
		}))
	}
	supersededBy, _ := fact.Metadata["superseded_by"].(string)
	if supersededBy != "" {
		reason, _ := fact.Metadata["supersede_reason"].(string)
		statements = append(statements, lstore.CreateRelationship(lstore.RelationshipSpec{
			FromLabel: "Fact",
			FromID:    supersededBy,
			Type:      "SUPERSEDES",
			ToLabel:   "Fact",
			ToID:      fact.ID,
			Props: map[string]string{
				"reason":     lstore.StringLiteral(reason),
				"created_at": lstore.TimeLiteral(fact.UpdatedAt),
			},
		}))
	}
	return statements
}

func deleteFactRelationshipStatements(fact Fact) []string {
	return []string{
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", FromID: fact.ID, Type: "SUBJECT", ToLabel: "Entity"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", FromID: fact.ID, Type: "OBJECT_ENTITY", ToLabel: "Entity"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Episode", Type: "ASSERTS", ToLabel: "Fact", ToID: fact.ID}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", FromID: fact.ID, Type: "SUPPORTED_BY", ToLabel: "Episode"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", FromID: fact.ID, Type: "SUPERSEDES", ToLabel: "Fact"}),
		lstore.DeleteRelationship(lstore.RelationshipSpec{FromLabel: "Fact", Type: "SUPERSEDES", ToLabel: "Fact", ToID: fact.ID}),
	}
}

func stripFactRelationshipFields(fact Fact) Fact {
	cloned := *cloneFact(fact)
	cloned.SubjectID = ""
	cloned.ObjectID = ""
	cloned.SupportingEpisodeIDs = nil
	if cloned.Metadata != nil {
		delete(cloned.Metadata, "superseded_by")
		delete(cloned.Metadata, "supersede_reason")
		if len(cloned.Metadata) == 0 {
			cloned.Metadata = nil
		}
	}
	return cloned
}

func episodeRelationshipsChanged(oldEpisode, newEpisode Episode) bool {
	return oldEpisode.SourceID != newEpisode.SourceID || !oldEpisode.IngestedAt.Equal(newEpisode.IngestedAt)
}

func factRelationshipsChanged(oldFact, newFact Fact) bool {
	if oldFact.SubjectID != newFact.SubjectID || oldFact.ObjectID != newFact.ObjectID {
		return true
	}
	if !oldFact.CreatedAt.Equal(newFact.CreatedAt) || !oldFact.UpdatedAt.Equal(newFact.UpdatedAt) {
		return true
	}
	if !sameStringSet(oldFact.SupportingEpisodeIDs, newFact.SupportingEpisodeIDs) {
		return true
	}
	oldSupersededBy, _ := oldFact.Metadata["superseded_by"].(string)
	newSupersededBy, _ := newFact.Metadata["superseded_by"].(string)
	oldReason, _ := oldFact.Metadata["supersede_reason"].(string)
	newReason, _ := newFact.Metadata["supersede_reason"].(string)
	return oldSupersededBy != newSupersededBy || oldReason != newReason
}

// decodeJSONMapStrict decodes a persisted JSON object and reports corruption
// instead of returning nil, so the migration reader can tell an absent value
// from a malformed one. The lenient read path keeps the historical behavior by
// ignoring the error and using the nil result.
func decodeJSONMapStrict(raw string) (map[string]any, error) {
	if isAbsentJSON(raw) {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// decodeJSONStringSliceStrict decodes a persisted JSON string array: a
// non-empty value that does not decode is corruption.
func decodeJSONStringSliceStrict(raw string) ([]string, error) {
	if isAbsentJSON(raw) {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func isAbsentJSON(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed == "" || trimmed == "null"
}

func asString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func asUint64(value any) uint64 {
	switch v := value.(type) {
	case nil:
		return 0
	case uint64:
		return v
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	default:
		return 0
	}
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func unionSortedKeys[T any, U any](left map[string]T, right map[string]U) []string {
	keys := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func clonePersistedState(state persistedState) persistedState {
	cloned := emptyPersistedState()
	cloned.Version = state.Version
	cloned.Sequence = state.Sequence
	for id, source := range state.Sources {
		cloned.Sources[id] = *cloneSource(source)
	}
	for id, episode := range state.Episodes {
		cloned.Episodes[id] = *cloneEpisode(episode)
	}
	for id, entity := range state.Entities {
		cloned.Entities[id] = *cloneEntity(entity)
	}
	for id, fact := range state.Facts {
		cloned.Facts[id] = *cloneFact(fact)
	}
	for id, revision := range state.FactRevisions {
		cloned.FactRevisions[id] = *cloneFactRevision(revision)
	}
	for id, revision := range state.EntityRevisions {
		cloned.EntityRevisions[id] = *cloneEntityRevision(revision)
	}
	for id, watermark := range state.MigrationWatermarks {
		cloned.MigrationWatermarks[id] = *cloneMigrationWatermark(watermark)
	}
	return cloned
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	slices.Sort(leftCopy)
	slices.Sort(rightCopy)
	return slices.Equal(leftCopy, rightCopy)
}
