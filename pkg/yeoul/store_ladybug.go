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

	state := emptyPersistedState()
	state.Version = currentStateVersion
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

func (s *ladybugStore) loadSources(state *persistedState) error {
	return s.loadNodes("Source", func(node lbug.Node) error {
		source := Source{
			ID:          asString(node.Properties["id"]),
			SpaceID:     asString(node.Properties["space_id"]),
			Kind:        asString(node.Properties["kind"]),
			URI:         asString(node.Properties["uri"]),
			ExternalRef: asString(node.Properties["external_ref"]),
			Metadata:    decodeJSONMap(asString(node.Properties["metadata_json"])),
			CreatedAt:   asTime(node.Properties["created_at"]),
		}
		state.Sources[source.ID] = source
		return nil
	})
}

func (s *ladybugStore) loadMeta(state *persistedState) error {
	return s.loadRows(lstore.QueryMetaSequence(), func(values []any) error {
		if len(values) > 0 {
			state.Sequence = asUint64(values[0])
		}
		return nil
	})
}

func (s *ladybugStore) loadEpisodes(state *persistedState) error {
	return s.loadNodes("Episode", func(node lbug.Node) error {
		episode := Episode{
			ID:         asString(node.Properties["id"]),
			SpaceID:    asString(node.Properties["space_id"]),
			Kind:       asString(node.Properties["kind"]),
			Content:    asString(node.Properties["content"]),
			SourceID:   asString(node.Properties["source_id"]),
			GroupID:    asString(node.Properties["group_id"]),
			ObservedAt: asTime(node.Properties["observed_at"]),
			IngestedAt: asTime(node.Properties["ingested_at"]),
			Metadata:   decodeJSONMap(asString(node.Properties["metadata_json"])),
		}
		state.Episodes[episode.ID] = episode
		return nil
	})
}

func (s *ladybugStore) loadEntities(state *persistedState) error {
	return s.loadNodes("Entity", func(node lbug.Node) error {
		entity := Entity{
			ID:            asString(node.Properties["id"]),
			SpaceID:       asString(node.Properties["space_id"]),
			Namespace:     asString(node.Properties["namespace"]),
			Type:          asString(node.Properties["type"]),
			CanonicalName: asString(node.Properties["canonical_name"]),
			Aliases:       decodeJSONStringSlice(asString(node.Properties["aliases_json"])),
			Metadata:      decodeJSONMap(asString(node.Properties["metadata_json"])),
			CreatedAt:     asTime(node.Properties["created_at"]),
			UpdatedAt:     asTime(node.Properties["updated_at"]),
		}
		state.Entities[entity.ID] = entity
		return nil
	})
}

func (s *ladybugStore) loadFacts(state *persistedState) error {
	return s.loadNodes("Fact", func(node lbug.Node) error {
		fact := Fact{
			ID:               asString(node.Properties["id"]),
			SpaceID:          asString(node.Properties["space_id"]),
			Predicate:        asString(node.Properties["predicate"]),
			ValueText:        asString(node.Properties["value_text"]),
			Confidence:       asFloat64(node.Properties["confidence"]),
			Status:           asString(node.Properties["status"]),
			ValidFrom:        asTime(node.Properties["valid_from"]),
			ValidTo:          asTime(node.Properties["valid_to"]),
			ObservedAt:       asTime(node.Properties["observed_at"]),
			CreatedAt:        asTime(node.Properties["created_at"]),
			UpdatedAt:        asTime(node.Properties["updated_at"]),
			RetractedAt:      asTime(node.Properties["retracted_at"]),
			RetractionReason: asString(node.Properties["retraction_reason"]),
			Metadata:         decodeJSONMap(asString(node.Properties["metadata_json"])),
		}
		state.Facts[fact.ID] = fact
		return nil
	})
}

func (s *ladybugStore) loadFactRevisions(state *persistedState) error {
	return s.loadNodes("FactRevision", func(node lbug.Node) error {
		revision := FactRevision{
			ID:                   asString(node.Properties["id"]),
			FactID:               asString(node.Properties["fact_id"]),
			SpaceID:              asString(node.Properties["space_id"]),
			RevisionKind:         asString(node.Properties["revision_kind"]),
			TxTime:               asTime(node.Properties["tx_time"]),
			Predicate:            asString(node.Properties["predicate"]),
			SubjectID:            asString(node.Properties["subject_id"]),
			ObjectID:             asString(node.Properties["object_id"]),
			ValueText:            asString(node.Properties["value_text"]),
			Confidence:           asFloat64(node.Properties["confidence"]),
			Status:               asString(node.Properties["status"]),
			ValidFrom:            asTime(node.Properties["valid_from"]),
			ValidTo:              asTime(node.Properties["valid_to"]),
			ObservedAt:           asTime(node.Properties["observed_at"]),
			CreatedAt:            asTime(node.Properties["created_at"]),
			UpdatedAt:            asTime(node.Properties["updated_at"]),
			RetractedAt:          asTime(node.Properties["retracted_at"]),
			RetractionReason:     asString(node.Properties["retraction_reason"]),
			SupportingEpisodeIDs: decodeJSONStringSlice(asString(node.Properties["supporting_episode_ids_json"])),
			Metadata:             decodeJSONMap(asString(node.Properties["metadata_json"])),
		}
		state.FactRevisions[revision.ID] = revision
		return nil
	})
}

func (s *ladybugStore) loadEntityRevisions(state *persistedState) error {
	return s.loadNodes("EntityRevision", func(node lbug.Node) error {
		revision := EntityRevision{
			ID:            asString(node.Properties["id"]),
			EntityID:      asString(node.Properties["entity_id"]),
			SpaceID:       asString(node.Properties["space_id"]),
			RevisionKind:  asString(node.Properties["revision_kind"]),
			TxTime:        asTime(node.Properties["tx_time"]),
			Namespace:     asString(node.Properties["namespace"]),
			Type:          asString(node.Properties["type"]),
			CanonicalName: asString(node.Properties["canonical_name"]),
			Aliases:       decodeJSONStringSlice(asString(node.Properties["aliases_json"])),
			Metadata:      decodeJSONMap(asString(node.Properties["metadata_json"])),
			CreatedAt:     asTime(node.Properties["created_at"]),
			UpdatedAt:     asTime(node.Properties["updated_at"]),
		}
		state.EntityRevisions[revision.ID] = revision
		return nil
	})
}

func (s *ladybugStore) loadMigrationWatermarks(state *persistedState) error {
	return s.loadNodes("YeoulMigration", func(node lbug.Node) error {
		watermark := MigrationWatermark{
			ID:        asString(node.Properties["id"]),
			AppliedAt: asTime(node.Properties["applied_at"]),
			Metadata:  decodeJSONMap(asString(node.Properties["metadata_json"])),
		}
		state.MigrationWatermarks[watermark.ID] = watermark
		return nil
	})
}

func (s *ladybugStore) loadSubjectEdges(state *persistedState) error {
	return s.loadRows(lstore.QuerySubjectEdges(), func(values []any) error {
		factID, entityID := asString(values[0]), asString(values[1])
		fact := state.Facts[factID]
		fact.SubjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadObjectEdges(state *persistedState) error {
	return s.loadRows(lstore.QueryObjectEdges(), func(values []any) error {
		factID, entityID := asString(values[0]), asString(values[1])
		fact := state.Facts[factID]
		fact.ObjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadSupportedByEdges(state *persistedState) error {
	return s.loadRows(lstore.QuerySupportedByEdges(), func(values []any) error {
		factID, episodeID := asString(values[0]), asString(values[1])
		fact := state.Facts[factID]
		if !slices.Contains(fact.SupportingEpisodeIDs, episodeID) {
			fact.SupportingEpisodeIDs = append(fact.SupportingEpisodeIDs, episodeID)
		}
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadSupersedesEdges(state *persistedState) error {
	return s.loadRows(lstore.QuerySupersedesEdges(), func(values []any) error {
		newID, oldID, reason := asString(values[0]), asString(values[1]), asString(values[2])
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

func (s *ladybugStore) loadNodes(label string, apply func(node lbug.Node) error) error {
	return s.loadRows(lstore.QueryNodes(label), func(values []any) error {
		if len(values) == 0 {
			return nil
		}
		node, ok := values[0].(lbug.Node)
		if !ok {
			return nil
		}
		return apply(node)
	})
}

func (s *ladybugStore) loadRows(query string, apply func(values []any) error) error {
	result, err := s.store.Query(query)
	if err != nil {
		if s.cfg.ReadOnly && lstore.IsMissingTableError(err) {
			return nil
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

func decodeJSONMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" || raw == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func decodeJSONStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" || raw == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
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

func asFloat64(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case float64:
		return v
	case float32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	default:
		return 0
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

func asTime(value any) time.Time {
	switch v := value.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return v.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return time.Time{}
		}
		return parsed.UTC()
	default:
		return time.Time{}
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
