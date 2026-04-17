package yeoul

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
)

type ladybugStore struct {
	cfg       Config
	store     *lstore.Store
	lastState persistedState
	loaded    bool
}

func newLadybugStore(cfg Config) (stateStore, error) {
	store, err := lstore.Open(cfg.DatabasePath, cfg.ReadOnly)
	if err != nil {
		return nil, errorf(ErrConfigInvalid, "open ladybug database", map[string]any{
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

	if err := s.exec(strings.Join(statements, ";\n")); err != nil {
		return errorf(ErrConfigInvalid, "write ladybug graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
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
	if err := s.exec(strings.Join(ladybugDDLStatements(), ";\n")); err != nil {
		return errorf(ErrConfigInvalid, "ensure ladybug graph schema", map[string]any{
			"database_path": s.cfg.DatabasePath,
		}, err)
	}
	return nil
}

func (s *ladybugStore) loadSources(state *persistedState) error {
	return s.loadNodes("MATCH (s:Source) RETURN s", func(node lbug.Node) error {
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
	return s.loadRows("MATCH (m:YeoulMeta {id: 'singleton'}) RETURN m.sequence", func(values []any) error {
		if len(values) > 0 {
			state.Sequence = asUint64(values[0])
		}
		return nil
	})
}

func (s *ladybugStore) loadEpisodes(state *persistedState) error {
	return s.loadNodes("MATCH (e:Episode) RETURN e", func(node lbug.Node) error {
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
	return s.loadNodes("MATCH (e:Entity) RETURN e", func(node lbug.Node) error {
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
	return s.loadNodes("MATCH (f:Fact) RETURN f", func(node lbug.Node) error {
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

func (s *ladybugStore) loadSubjectEdges(state *persistedState) error {
	query := "MATCH (f:Fact)-[:SUBJECT]->(e:Entity) RETURN f.id, e.id"
	return s.loadRows(query, func(values []any) error {
		factID, entityID := asString(values[0]), asString(values[1])
		fact := state.Facts[factID]
		fact.SubjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadObjectEdges(state *persistedState) error {
	query := "MATCH (f:Fact)-[:OBJECT_ENTITY]->(e:Entity) RETURN f.id, e.id"
	return s.loadRows(query, func(values []any) error {
		factID, entityID := asString(values[0]), asString(values[1])
		fact := state.Facts[factID]
		fact.ObjectID = entityID
		state.Facts[factID] = fact
		return nil
	})
}

func (s *ladybugStore) loadSupportedByEdges(state *persistedState) error {
	query := "MATCH (f:Fact)-[:SUPPORTED_BY]->(e:Episode) RETURN f.id, e.id"
	return s.loadRows(query, func(values []any) error {
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
	query := "MATCH (newFact:Fact)-[r:SUPERSEDES]->(oldFact:Fact) RETURN newFact.id, oldFact.id, r.reason"
	return s.loadRows(query, func(values []any) error {
		newID, oldID, reason := asString(values[0]), asString(values[1]), asString(values[2])
		oldFact := state.Facts[oldID]
		oldFact.Metadata = mergeAnyMap(oldFact.Metadata, map[string]any{
			"superseded_by":    newID,
			"supersede_reason": reason,
		})
		state.Facts[oldID] = oldFact
		newFact := state.Facts[newID]
		newFact.Metadata = mergeAnyMap(newFact.Metadata, map[string]any{
			"supersedes":       oldID,
			"supersede_reason": reason,
		})
		state.Facts[newID] = newFact
		return nil
	})
}

func (s *ladybugStore) loadNodes(query string, apply func(node lbug.Node) error) error {
	return s.loadRows(query, func(values []any) error {
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
		if s.cfg.ReadOnly && isMissingTableError(err) {
			return nil
		}
		return errorf(ErrConfigInvalid, "query ladybug graph state", map[string]any{
			"database_path": s.cfg.DatabasePath,
			"query":         query,
		}, err)
	}
	defer result.Close()

	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return errorf(ErrConfigInvalid, "read ladybug graph row", map[string]any{
				"database_path": s.cfg.DatabasePath,
				"query":         query,
			}, err)
		}
		values, err := tuple.GetAsSlice()
		if err != nil {
			return errorf(ErrConfigInvalid, "decode ladybug graph row", map[string]any{
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

func (s *ladybugStore) exec(query string) error {
	result, err := s.store.Query(query)
	if result != nil {
		result.Close()
	}
	return err
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

	statements = append(statements, s.buildEpisodeRelationshipDelta(prev.Episodes, next.Episodes)...)
	statements = append(statements, s.buildFactRelationshipDelta(prev.Facts, next.Facts)...)
	return statements
}

func (s *ladybugStore) buildMetaDelta(prev, next persistedState) []string {
	if prev.Sequence == next.Sequence && s.loaded {
		return nil
	}
	return []string{
		"MATCH (m:YeoulMeta {id:'singleton'}) DELETE m",
		fmt.Sprintf("CREATE (:YeoulMeta {id:'singleton', sequence:%s})", cypherUint64Literal(next.Sequence)),
	}
}

func (s *ladybugStore) buildSourceDelta(prev, next map[string]Source) []string {
	statements := make([]string, 0)
	for _, id := range unionSortedKeys(prev, next) {
		oldSource, oldOK := prev[id]
		newSource, newOK := next[id]
		switch {
		case !oldOK && newOK:
			statements = append(statements, createSourceStatement(newSource))
		case oldOK && newOK && !reflect.DeepEqual(oldSource, newSource):
			statements = append(statements, updateSourceStatement(newSource))
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
			statements = append(statements, createEpisodeStatement(newEpisode))
		case oldOK && newOK && !reflect.DeepEqual(oldEpisode, newEpisode):
			statements = append(statements, updateEpisodeStatement(newEpisode))
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
			statements = append(statements, createEntityStatement(newEntity))
		case oldOK && newOK && !reflect.DeepEqual(oldEntity, newEntity):
			statements = append(statements, updateEntityStatement(newEntity))
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
			statements = append(statements, createFactStatement(newFact))
		case oldOK && newOK && !reflect.DeepEqual(stripFactRelationshipFields(oldFact), stripFactRelationshipFields(newFact)):
			statements = append(statements, updateFactStatement(newFact))
		}
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
		statements = append(statements, deleteFactNodeStatement(id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedEpisodeStatements(prev, next map[string]Episode) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, deleteEpisodeNodeStatement(id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedEntityStatements(prev, next map[string]Entity) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, deleteEntityNodeStatement(id))
	}
	return statements
}

func (s *ladybugStore) buildDeletedSourceStatements(prev, next map[string]Source) []string {
	statements := make([]string, 0)
	for _, id := range sortedKeys(prev) {
		if _, ok := next[id]; ok {
			continue
		}
		statements = append(statements, deleteSourceNodeStatement(id))
	}
	return statements
}

func ladybugDDLStatements() []string {
	return []string{
		"CREATE NODE TABLE IF NOT EXISTS YeoulMeta(id STRING, sequence INT64, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Source(id STRING, space_id STRING, kind STRING, uri STRING, external_ref STRING, created_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Episode(id STRING, space_id STRING, kind STRING, content STRING, content_hash STRING, source_id STRING, group_id STRING, observed_at TIMESTAMP, ingested_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Entity(id STRING, space_id STRING, namespace STRING, type STRING, canonical_name STRING, aliases_json STRING, fingerprint STRING, created_at TIMESTAMP, updated_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Fact(id STRING, space_id STRING, predicate STRING, value_text STRING, confidence DOUBLE, status STRING, valid_from TIMESTAMP, valid_to TIMESTAMP, observed_at TIMESTAMP, created_at TIMESTAMP, updated_at TIMESTAMP, retracted_at TIMESTAMP, retraction_reason STRING, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE REL TABLE IF NOT EXISTS FROM_SOURCE(FROM Episode TO Source, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS ASSERTS(FROM Episode TO Fact, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUBJECT(FROM Fact TO Entity, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS OBJECT_ENTITY(FROM Fact TO Entity, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUPPORTED_BY(FROM Fact TO Episode, support_kind STRING, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUPERSEDES(FROM Fact TO Fact, reason STRING, created_at TIMESTAMP)",
	}
}

func createSourceStatement(source Source) string {
	return fmt.Sprintf(
		"CREATE (:Source {id:%s, space_id:%s, kind:%s, uri:%s, external_ref:%s, created_at:%s, metadata_json:%s})",
		cypherStringLiteral(source.ID),
		cypherStringLiteral(source.SpaceID),
		cypherStringLiteral(source.Kind),
		cypherStringLiteral(source.URI),
		cypherStringLiteral(source.ExternalRef),
		cypherTimeLiteral(source.CreatedAt),
		cypherJSONLiteral(source.Metadata),
	)
}

func updateSourceStatement(source Source) string {
	return fmt.Sprintf(
		"MATCH (s:Source {id:%s}) SET s.space_id=%s, s.kind=%s, s.uri=%s, s.external_ref=%s, s.created_at=%s, s.metadata_json=%s",
		cypherStringLiteral(source.ID),
		cypherStringLiteral(source.SpaceID),
		cypherStringLiteral(source.Kind),
		cypherStringLiteral(source.URI),
		cypherStringLiteral(source.ExternalRef),
		cypherTimeLiteral(source.CreatedAt),
		cypherJSONLiteral(source.Metadata),
	)
}

func createEpisodeStatement(episode Episode) string {
	return fmt.Sprintf(
		"CREATE (:Episode {id:%s, space_id:%s, kind:%s, content:%s, content_hash:%s, source_id:%s, group_id:%s, observed_at:%s, ingested_at:%s, metadata_json:%s})",
		cypherStringLiteral(episode.ID),
		cypherStringLiteral(episode.SpaceID),
		cypherStringLiteral(episode.Kind),
		cypherStringLiteral(episode.Content),
		cypherStringLiteral(""),
		cypherStringLiteral(episode.SourceID),
		cypherStringLiteral(episode.GroupID),
		cypherTimeLiteral(episode.ObservedAt),
		cypherTimeLiteral(episode.IngestedAt),
		cypherJSONLiteral(episode.Metadata),
	)
}

func updateEpisodeStatement(episode Episode) string {
	return fmt.Sprintf(
		"MATCH (e:Episode {id:%s}) SET e.space_id=%s, e.kind=%s, e.content=%s, e.content_hash=%s, e.source_id=%s, e.group_id=%s, e.observed_at=%s, e.ingested_at=%s, e.metadata_json=%s",
		cypherStringLiteral(episode.ID),
		cypherStringLiteral(episode.SpaceID),
		cypherStringLiteral(episode.Kind),
		cypherStringLiteral(episode.Content),
		cypherStringLiteral(""),
		cypherStringLiteral(episode.SourceID),
		cypherStringLiteral(episode.GroupID),
		cypherTimeLiteral(episode.ObservedAt),
		cypherTimeLiteral(episode.IngestedAt),
		cypherJSONLiteral(episode.Metadata),
	)
}

func createEntityStatement(entity Entity) string {
	return fmt.Sprintf(
		"CREATE (:Entity {id:%s, space_id:%s, namespace:%s, type:%s, canonical_name:%s, aliases_json:%s, fingerprint:%s, created_at:%s, updated_at:%s, metadata_json:%s})",
		cypherStringLiteral(entity.ID),
		cypherStringLiteral(entity.SpaceID),
		cypherStringLiteral(entity.Namespace),
		cypherStringLiteral(entity.Type),
		cypherStringLiteral(entity.CanonicalName),
		cypherJSONLiteral(entity.Aliases),
		cypherStringLiteral(""),
		cypherTimeLiteral(entity.CreatedAt),
		cypherTimeLiteral(entity.UpdatedAt),
		cypherJSONLiteral(entity.Metadata),
	)
}

func updateEntityStatement(entity Entity) string {
	return fmt.Sprintf(
		"MATCH (e:Entity {id:%s}) SET e.space_id=%s, e.namespace=%s, e.type=%s, e.canonical_name=%s, e.aliases_json=%s, e.fingerprint=%s, e.created_at=%s, e.updated_at=%s, e.metadata_json=%s",
		cypherStringLiteral(entity.ID),
		cypherStringLiteral(entity.SpaceID),
		cypherStringLiteral(entity.Namespace),
		cypherStringLiteral(entity.Type),
		cypherStringLiteral(entity.CanonicalName),
		cypherJSONLiteral(entity.Aliases),
		cypherStringLiteral(""),
		cypherTimeLiteral(entity.CreatedAt),
		cypherTimeLiteral(entity.UpdatedAt),
		cypherJSONLiteral(entity.Metadata),
	)
}

func createFactStatement(fact Fact) string {
	return fmt.Sprintf(
		"CREATE (:Fact {id:%s, space_id:%s, predicate:%s, value_text:%s, confidence:%s, status:%s, valid_from:%s, valid_to:%s, observed_at:%s, created_at:%s, updated_at:%s, retracted_at:%s, retraction_reason:%s, metadata_json:%s})",
		cypherStringLiteral(fact.ID),
		cypherStringLiteral(fact.SpaceID),
		cypherStringLiteral(fact.Predicate),
		cypherStringLiteral(fact.ValueText),
		cypherFloatLiteral(fact.Confidence),
		cypherStringLiteral(fact.Status),
		cypherTimeLiteral(fact.ValidFrom),
		cypherTimeLiteral(fact.ValidTo),
		cypherTimeLiteral(fact.ObservedAt),
		cypherTimeLiteral(fact.CreatedAt),
		cypherTimeLiteral(fact.UpdatedAt),
		cypherTimeLiteral(fact.RetractedAt),
		cypherStringLiteral(fact.RetractionReason),
		cypherJSONLiteral(fact.Metadata),
	)
}

func updateFactStatement(fact Fact) string {
	return fmt.Sprintf(
		"MATCH (f:Fact {id:%s}) SET f.space_id=%s, f.predicate=%s, f.value_text=%s, f.confidence=%s, f.status=%s, f.valid_from=%s, f.valid_to=%s, f.observed_at=%s, f.created_at=%s, f.updated_at=%s, f.retracted_at=%s, f.retraction_reason=%s, f.metadata_json=%s",
		cypherStringLiteral(fact.ID),
		cypherStringLiteral(fact.SpaceID),
		cypherStringLiteral(fact.Predicate),
		cypherStringLiteral(fact.ValueText),
		cypherFloatLiteral(fact.Confidence),
		cypherStringLiteral(fact.Status),
		cypherTimeLiteral(fact.ValidFrom),
		cypherTimeLiteral(fact.ValidTo),
		cypherTimeLiteral(fact.ObservedAt),
		cypherTimeLiteral(fact.CreatedAt),
		cypherTimeLiteral(fact.UpdatedAt),
		cypherTimeLiteral(fact.RetractedAt),
		cypherStringLiteral(fact.RetractionReason),
		cypherJSONLiteral(fact.Metadata),
	)
}

func deleteSourceNodeStatement(id string) string {
	return fmt.Sprintf("MATCH (s:Source {id:%s}) DELETE s", cypherStringLiteral(id))
}

func deleteEpisodeNodeStatement(id string) string {
	return fmt.Sprintf("MATCH (e:Episode {id:%s}) DELETE e", cypherStringLiteral(id))
}

func deleteEntityNodeStatement(id string) string {
	return fmt.Sprintf("MATCH (e:Entity {id:%s}) DELETE e", cypherStringLiteral(id))
}

func deleteFactNodeStatement(id string) string {
	return fmt.Sprintf("MATCH (f:Fact {id:%s}) DELETE f", cypherStringLiteral(id))
}

func createEpisodeRelationshipStatements(episode Episode) []string {
	if episode.SourceID == "" {
		return nil
	}
	return []string{
		fmt.Sprintf(
			"MATCH (e:Episode {id:%s}), (src:Source {id:%s}) CREATE (e)-[:FROM_SOURCE {created_at:%s}]->(src)",
			cypherStringLiteral(episode.ID),
			cypherStringLiteral(episode.SourceID),
			cypherTimeLiteral(episode.IngestedAt),
		),
	}
}

func deleteEpisodeRelationshipStatements(episode Episode) []string {
	return []string{
		fmt.Sprintf("MATCH (:Episode {id:%s})-[r:FROM_SOURCE]->(:Source) DELETE r", cypherStringLiteral(episode.ID)),
		fmt.Sprintf("MATCH (:Episode {id:%s})-[r:ASSERTS]->(:Fact) DELETE r", cypherStringLiteral(episode.ID)),
		fmt.Sprintf("MATCH (:Fact)-[r:SUPPORTED_BY]->(:Episode {id:%s}) DELETE r", cypherStringLiteral(episode.ID)),
	}
}

func createFactRelationshipStatements(fact Fact) []string {
	statements := make([]string, 0, 6+len(fact.SupportingEpisodeIDs)*2)
	if fact.SubjectID != "" {
		statements = append(statements, fmt.Sprintf(
			"MATCH (f:Fact {id:%s}), (e:Entity {id:%s}) CREATE (f)-[:SUBJECT {created_at:%s}]->(e)",
			cypherStringLiteral(fact.ID),
			cypherStringLiteral(fact.SubjectID),
			cypherTimeLiteral(fact.CreatedAt),
		))
	}
	if fact.ObjectID != "" {
		statements = append(statements, fmt.Sprintf(
			"MATCH (f:Fact {id:%s}), (e:Entity {id:%s}) CREATE (f)-[:OBJECT_ENTITY {created_at:%s}]->(e)",
			cypherStringLiteral(fact.ID),
			cypherStringLiteral(fact.ObjectID),
			cypherTimeLiteral(fact.CreatedAt),
		))
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		statements = append(statements, fmt.Sprintf(
			"MATCH (ep:Episode {id:%s}), (f:Fact {id:%s}) CREATE (ep)-[:ASSERTS {created_at:%s}]->(f)",
			cypherStringLiteral(episodeID),
			cypherStringLiteral(fact.ID),
			cypherTimeLiteral(fact.CreatedAt),
		))
		statements = append(statements, fmt.Sprintf(
			"MATCH (f:Fact {id:%s}), (ep:Episode {id:%s}) CREATE (f)-[:SUPPORTED_BY {support_kind:%s, created_at:%s}]->(ep)",
			cypherStringLiteral(fact.ID),
			cypherStringLiteral(episodeID),
			cypherStringLiteral("observed"),
			cypherTimeLiteral(fact.CreatedAt),
		))
	}
	supersededBy, _ := fact.Metadata["superseded_by"].(string)
	if supersededBy != "" {
		reason, _ := fact.Metadata["supersede_reason"].(string)
		statements = append(statements, fmt.Sprintf(
			"MATCH (newFact:Fact {id:%s}), (oldFact:Fact {id:%s}) CREATE (newFact)-[:SUPERSEDES {reason:%s, created_at:%s}]->(oldFact)",
			cypherStringLiteral(supersededBy),
			cypherStringLiteral(fact.ID),
			cypherStringLiteral(reason),
			cypherTimeLiteral(fact.UpdatedAt),
		))
	}
	return statements
}

func deleteFactRelationshipStatements(fact Fact) []string {
	return []string{
		fmt.Sprintf("MATCH (:Fact {id:%s})-[r:SUBJECT]->(:Entity) DELETE r", cypherStringLiteral(fact.ID)),
		fmt.Sprintf("MATCH (:Fact {id:%s})-[r:OBJECT_ENTITY]->(:Entity) DELETE r", cypherStringLiteral(fact.ID)),
		fmt.Sprintf("MATCH (:Episode)-[r:ASSERTS]->(:Fact {id:%s}) DELETE r", cypherStringLiteral(fact.ID)),
		fmt.Sprintf("MATCH (:Fact {id:%s})-[r:SUPPORTED_BY]->(:Episode) DELETE r", cypherStringLiteral(fact.ID)),
		fmt.Sprintf("MATCH (:Fact {id:%s})-[r:SUPERSEDES]->(:Fact) DELETE r", cypherStringLiteral(fact.ID)),
		fmt.Sprintf("MATCH (:Fact)-[r:SUPERSEDES]->(:Fact {id:%s}) DELETE r", cypherStringLiteral(fact.ID)),
	}
}

func cypherStringLiteral(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func cypherJSONLiteral(value any) string {
	data, _ := json.Marshal(value)
	return cypherStringLiteral(string(data))
}

func cypherTimeLiteral(value time.Time) string {
	if value.IsZero() {
		return "NULL"
	}
	return fmt.Sprintf("timestamp(%s)", cypherStringLiteral(value.UTC().Format(time.RFC3339Nano)))
}

func cypherFloatLiteral(value float64) string {
	return fmt.Sprintf("%g", value)
}

func cypherUint64Literal(value uint64) string {
	return fmt.Sprintf("%d", value)
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

func isMissingTableError(err error) bool {
	return strings.Contains(err.Error(), "Binder exception: Table ")
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
