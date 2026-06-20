package yeoul

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	factStatusActive     = "active"
	factStatusSuperseded = "superseded"
	factStatusRetracted  = "retracted"
	factCardinalityOne   = "one"
	factCardinalityMany  = "many"
	entityHistoryKey     = "_history"
	bitemporalWatermark  = "bitemporal_revision_seed_v1"
)

var reservedFactMetadataKeys = map[string]bool{
	"superseded_by":    true,
	"supersedes":       true,
	"supersede_reason": true,
	"duplicate_of":     true,
	entityHistoryKey:   true,
}

type engine struct {
	mu       sync.RWMutex
	now      func() time.Time
	sequence uint64
	cfg      Config
	store    stateStore

	sources  map[string]Source
	episodes map[string]Episode
	entities map[string]Entity
	facts    map[string]Fact

	factRevisions       map[string]FactRevision
	entityRevisions     map[string]EntityRevision
	migrationWatermarks map[string]MigrationWatermark
}

func Open(ctx context.Context, cfg Config) (Engine, error) {
	_ = ctx

	if cfg.InMemory && cfg.DatabasePath != "" {
		return nil, errorf(ErrConfigInvalid, "database_path must be empty when in_memory is true", nil, nil)
	}
	if !cfg.InMemory && cfg.DatabasePath == "" {
		return nil, errorf(ErrConfigInvalid, "database_path is required when in_memory is false", nil, nil)
	}

	store, err := openStateStore(cfg)
	if err != nil {
		return nil, err
	}

	state, err := store.Load()
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	eng := newEngine(cfg, store)
	eng.applyState(*state)
	return eng, nil
}

func newEngine(cfg Config, store stateStore) *engine {
	return &engine{
		now: func() time.Time {
			return time.Now().UTC()
		},
		cfg:                 cfg,
		store:               store,
		sources:             make(map[string]Source),
		episodes:            make(map[string]Episode),
		entities:            make(map[string]Entity),
		facts:               make(map[string]Fact),
		factRevisions:       make(map[string]FactRevision),
		entityRevisions:     make(map[string]EntityRevision),
		migrationWatermarks: make(map[string]MigrationWatermark),
	}
}

func (e *engine) Close(ctx context.Context) error {
	_ = ctx
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.saveLocked(); err != nil {
		return err
	}
	if e.store == nil {
		return nil
	}
	return e.store.Close()
}

func (e *engine) ensureWritableLocked() error {
	if e.cfg.ReadOnly {
		return errorf(ErrNotSupported, "write operation is not allowed in read-only mode", map[string]any{
			"database_path": e.cfg.DatabasePath,
		}, nil)
	}
	return nil
}

func (e *engine) IngestEpisode(ctx context.Context, input EpisodeInput) (*EpisodeResult, error) {
	_ = ctx
	if strings.TrimSpace(input.Kind) == "" {
		return nil, errorf(ErrInputInvalid, "episode kind is required", map[string]any{"field": "kind"}, nil)
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, errorf(ErrInputInvalid, "episode content is required", map[string]any{"field": "content"}, nil)
	}

	now := e.now()
	spaceID := normalizeSpaceID(input.SpaceID)

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
	source, err := e.resolveSource(spaceID, input.SourceID, input.Source, now)
	if err != nil {
		return nil, err
	}
	snapshot := e.snapshotLocked()
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}
	result, err := e.ingestEpisodeLocked(spaceID, input, source, now)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}
	return result, nil
}

func (e *engine) IngestBatch(ctx context.Context, input BatchInput) (*BatchResult, error) {
	_ = ctx
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}

	snapshot := e.snapshotLocked()
	result := &BatchResult{}
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}

	for _, episode := range input.Episodes {
		if strings.TrimSpace(episode.Kind) == "" || strings.TrimSpace(episode.Content) == "" {
			restore()
			return nil, errorf(ErrInputInvalid, "episode kind and content are required", map[string]any{"kind": episode.Kind, "id": episode.ID}, nil)
		}
		now := e.now()
		spaceID := normalizeSpaceID(episode.SpaceID)
		source, err := e.resolveSource(spaceID, episode.SourceID, episode.Source, now)
		if err != nil {
			restore()
			return nil, err
		}
		item, err := e.ingestEpisodeLocked(spaceID, episode, source, now)
		if err != nil {
			restore()
			return nil, err
		}
		result.EpisodeIDs = append(result.EpisodeIDs, item.EpisodeID)
	}
	for _, entity := range input.Entities {
		if strings.TrimSpace(entity.Type) == "" || strings.TrimSpace(entity.CanonicalName) == "" {
			restore()
			return nil, errorf(ErrInputInvalid, "entity type and canonical_name are required", map[string]any{"id": entity.ID}, nil)
		}
		item, err := e.upsertEntityLocked(entity)
		if err != nil {
			restore()
			return nil, err
		}
		result.EntityIDs = append(result.EntityIDs, item.ID)
	}
	for _, fact := range input.Facts {
		if strings.TrimSpace(fact.Predicate) == "" || strings.TrimSpace(fact.SubjectID) == "" || len(fact.SupportingEpisodeIDs) == 0 {
			restore()
			return nil, errorf(ErrInputInvalid, "fact predicate, subject_id, and supporting_episode_ids are required", map[string]any{"id": fact.ID}, nil)
		}
		item, err := e.assertFactLocked(normalizeSpaceID(fact.SpaceID), fact, false)
		if err != nil {
			restore()
			return nil, err
		}
		result.FactIDs = append(result.FactIDs, item.ID)
	}
	if len(input.EntityRevisions) > 0 || len(input.FactRevisions) > 0 {
		restore()
		return nil, errorf(ErrLifecycleInvalid, "revision import is not supported by public batch ingest", nil, nil)
	}
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}
	return result, nil
}

func (e *engine) UpsertEntity(ctx context.Context, input EntityInput) (*Entity, error) {
	_ = ctx
	if strings.TrimSpace(input.Type) == "" {
		return nil, errorf(ErrInputInvalid, "entity type is required", map[string]any{"field": "type"}, nil)
	}
	if strings.TrimSpace(input.CanonicalName) == "" {
		return nil, errorf(ErrInputInvalid, "entity canonical_name is required", map[string]any{"field": "canonical_name"}, nil)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
	snapshot := e.snapshotLocked()
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}
	entity, err := e.upsertEntityLocked(input)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}
	return entity, nil
}

func (e *engine) ingestEpisodeLocked(spaceID string, input EpisodeInput, source Source, now time.Time) (*EpisodeResult, error) {
	if existing, ok := e.sources[source.ID]; ok {
		if existing.SpaceID != spaceID {
			return nil, errorf(ErrLifecycleInvalid, "source id already exists in another space", map[string]any{"source_id": source.ID, "space_id": spaceID}, nil)
		}
	} else {
		e.sources[source.ID] = source
	}

	episodeID := input.ID
	created := false
	if episodeID == "" {
		episodeID = e.newIDLocked("ep")
	}
	if existing, ok := e.episodes[episodeID]; ok {
		if existing.SpaceID != spaceID || existing.Kind != input.Kind || existing.Content != input.Content || existing.SourceID != source.ID || existing.GroupID != input.GroupID {
			return nil, errorf(ErrLifecycleInvalid, "episode id already exists with different content", map[string]any{
				"episode_id": episodeID,
			}, nil)
		}
	} else {
		created = true
		e.episodes[episodeID] = Episode{
			ID:         episodeID,
			SpaceID:    spaceID,
			Kind:       input.Kind,
			Content:    input.Content,
			SourceID:   source.ID,
			GroupID:    input.GroupID,
			ObservedAt: input.ObservedAt,
			IngestedAt: now,
			Metadata:   cloneAnyMap(input.Metadata),
		}
	}
	return &EpisodeResult{
		EpisodeID: episodeID,
		SourceID:  source.ID,
		Created:   created,
	}, nil
}

func (e *engine) upsertEntityLocked(input EntityInput) (*Entity, error) {
	now := e.now()
	spaceID := normalizeSpaceID(input.SpaceID)
	id := input.ID
	if id == "" {
		id = normalizeEntityID(input.Namespace, input.Type, firstNonEmpty(input.StableKey, input.CanonicalName))
	}
	metadata := cloneAnyMap(input.Metadata)
	if strings.TrimSpace(input.StableKey) != "" {
		metadata = mergeAnyMap(metadata, map[string]any{"stable_key": input.StableKey})
	}

	entity, ok := e.entities[id]
	if !ok {
		entity = Entity{
			ID:            id,
			SpaceID:       spaceID,
			Namespace:     input.Namespace,
			Type:          input.Type,
			CanonicalName: input.CanonicalName,
			Aliases:       dedupeStrings(input.Aliases),
			Metadata:      metadata,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		e.entities[id] = entity
		e.appendEntityRevisionLocked(entity, "assert")
		return cloneEntity(entity), nil
	}

	if entity.SpaceID != "" && entity.SpaceID != spaceID {
		return nil, errorf(ErrLifecycleInvalid, "entity id already exists in another space", map[string]any{
			"entity_id": id,
		}, nil)
	}
	if entity.Namespace != "" && input.Namespace != "" && entity.Namespace != input.Namespace {
		return nil, errorf(ErrLifecycleInvalid, "entity namespace is immutable", map[string]any{"entity_id": id}, nil)
	}
	if entity.Type != "" && input.Type != "" && entity.Type != input.Type {
		return nil, errorf(ErrLifecycleInvalid, "entity type is immutable", map[string]any{"entity_id": id}, nil)
	}
	entity.Metadata = appendEntityHistory(entity.Metadata, entitySnapshotEntry(entity, now))
	entity.SpaceID = firstNonEmpty(entity.SpaceID, spaceID)
	entity.Namespace = firstNonEmpty(entity.Namespace, input.Namespace)
	entity.Type = firstNonEmpty(entity.Type, input.Type)
	entity.CanonicalName = input.CanonicalName
	entity.Aliases = dedupeStrings(append(entity.Aliases, input.Aliases...))
	entity.Metadata = mergeAnyMap(entity.Metadata, metadata)
	entity.UpdatedAt = now
	e.entities[id] = entity
	e.appendEntityRevisionLocked(entity, "update")
	return cloneEntity(entity), nil
}

func (e *engine) AssertFact(ctx context.Context, input FactInput) (*Fact, error) {
	_ = ctx
	if strings.TrimSpace(input.Predicate) == "" {
		return nil, errorf(ErrInputInvalid, "fact predicate is required", map[string]any{"field": "predicate"}, nil)
	}
	if strings.TrimSpace(input.SubjectID) == "" {
		return nil, errorf(ErrInputInvalid, "fact subject_id is required", map[string]any{"field": "subject_id"}, nil)
	}

	if len(input.SupportingEpisodeIDs) == 0 {
		return nil, errorf(ErrInputInvalid, "supporting_episode_ids must contain at least one episode", map[string]any{"field": "supporting_episode_ids"}, nil)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}

	spaceID := normalizeSpaceID(input.SpaceID)
	snapshot := e.snapshotLocked()
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}
	fact, err := e.assertFactLocked(spaceID, input, false)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}
	return fact, nil
}

func (e *engine) assertFactLocked(spaceID string, input FactInput, allowLifecycleFields bool) (*Fact, error) {
	if _, ok := e.entities[input.SubjectID]; !ok {
		return nil, errorf(ErrEntityNotFound, "subject entity not found", map[string]any{"subject_id": input.SubjectID}, nil)
	}
	if entity := e.entities[input.SubjectID]; entity.SpaceID != spaceID {
		return nil, errorf(ErrEntityNotFound, "subject entity not found in space", map[string]any{"subject_id": input.SubjectID, "space_id": spaceID}, nil)
	}
	if input.ObjectID != "" {
		if _, ok := e.entities[input.ObjectID]; !ok {
			return nil, errorf(ErrEntityNotFound, "object entity not found", map[string]any{"object_id": input.ObjectID}, nil)
		}
		if entity := e.entities[input.ObjectID]; entity.SpaceID != spaceID {
			return nil, errorf(ErrEntityNotFound, "object entity not found in space", map[string]any{"object_id": input.ObjectID, "space_id": spaceID}, nil)
		}
	}
	for _, episodeID := range input.SupportingEpisodeIDs {
		episode, ok := e.episodes[episodeID]
		if !ok {
			return nil, errorf(ErrInputInvalid, "supporting episode not found", map[string]any{"episode_id": episodeID}, nil)
		}
		if episode.SpaceID != spaceID {
			return nil, errorf(ErrInputInvalid, "supporting episode not found in space", map[string]any{"episode_id": episodeID, "space_id": spaceID}, nil)
		}
	}

	now := e.now()
	id := input.ID
	if id == "" {
		id = e.newIDLocked("fact")
	}
	if _, ok := e.facts[id]; ok {
		return nil, errorf(ErrLifecycleInvalid, "fact id already exists", map[string]any{"fact_id": id}, nil)
	}

	status := input.Status
	if status == "" {
		status = factStatusActive
	}
	if !validFactStatus(status) {
		return nil, errorf(ErrInputInvalid, "invalid fact status", map[string]any{"status": status}, nil)
	}
	if !allowLifecycleFields && status != factStatusActive {
		return nil, errorf(ErrLifecycleInvalid, "fact status is lifecycle-managed; use supersede or retract", map[string]any{"status": status}, nil)
	}
	if !allowLifecycleFields {
		for key := range input.Metadata {
			if reservedFactMetadataKeys[key] {
				return nil, errorf(ErrLifecycleInvalid, "fact metadata key is lifecycle-managed", map[string]any{"metadata_key": key}, nil)
			}
		}
	}
	cardinality := strings.TrimSpace(input.Cardinality)
	if cardinality == "" {
		cardinality = factCardinalityMany
	}
	if cardinality != factCardinalityOne && cardinality != factCardinalityMany {
		return nil, errorf(ErrInputInvalid, "invalid fact cardinality", map[string]any{"cardinality": input.Cardinality}, nil)
	}
	if !validFactInterval(input.ValidFrom, input.ValidTo) {
		return nil, errorf(ErrInputInvalid, "invalid fact validity interval", map[string]any{"fact_id": id}, nil)
	}
	fact := Fact{
		ID:                   id,
		SpaceID:              spaceID,
		Predicate:            input.Predicate,
		SubjectID:            input.SubjectID,
		ObjectID:             input.ObjectID,
		ValueText:            input.ValueText,
		Confidence:           input.Confidence,
		Status:               status,
		ValidFrom:            input.ValidFrom,
		ValidTo:              input.ValidTo,
		ObservedAt:           input.ObservedAt,
		CreatedAt:            now,
		UpdatedAt:            now,
		SupportingEpisodeIDs: dedupeStrings(input.SupportingEpisodeIDs),
		Metadata:             cloneAnyMap(input.Metadata),
	}
	if !allowLifecycleFields && cardinality == factCardinalityOne {
		fact = e.invalidateFactSlotLocked(fact, input.ValidFrom)
	}
	e.facts[id] = fact
	e.appendFactRevisionLocked(fact, "assert")
	return cloneFact(fact), nil
}

func (e *engine) invalidateFactSlotLocked(newFact Fact, validTo time.Time) Fact {
	superseded := make([]string, 0, 1)
	for id, fact := range e.facts {
		if fact.Status != factStatusActive || fact.SpaceID != newFact.SpaceID || fact.SubjectID != newFact.SubjectID || fact.Predicate != newFact.Predicate {
			continue
		}
		if !factValidityOverlaps(fact.ValidFrom, fact.ValidTo, newFact.ValidFrom, newFact.ValidTo) {
			continue
		}
		fact.Status = factStatusSuperseded
		if !validTo.IsZero() && fact.ValidFrom.Before(validTo) && (fact.ValidTo.IsZero() || validTo.Before(fact.ValidTo)) {
			fact.ValidTo = validTo
		}
		fact.UpdatedAt = newFact.CreatedAt
		fact.Metadata = mergeAnyMap(fact.Metadata, map[string]any{
			"superseded_by":    newFact.ID,
			"supersede_reason": "cardinality_one_slot_replaced",
		})
		e.facts[id] = fact
		e.appendFactRevisionLocked(fact, "auto_supersede")
		superseded = append(superseded, id)
	}
	if len(superseded) == 0 {
		return newFact
	}
	newFact.Metadata = mergeAnyMap(newFact.Metadata, map[string]any{
		"supersedes":       superseded,
		"supersede_reason": "cardinality_one_slot_replaced",
	})
	return newFact
}

func (e *engine) SupersedeFact(ctx context.Context, factID string, input FactInput, reason string) (*SupersedeFactResult, error) {
	_ = ctx
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
	snapshot := e.snapshotLocked()
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}
	result, err := e.supersedeFactLocked(factID, input, reason)
	if err != nil {
		restore()
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}
	return result, nil
}

func (e *engine) supersedeFactLocked(factID string, input FactInput, reason string) (*SupersedeFactResult, error) {
	oldFact, ok := e.facts[factID]
	if !ok {
		return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": factID}, nil)
	}
	if oldFact.Status == factStatusRetracted {
		return nil, errorf(ErrLifecycleInvalid, "retracted fact cannot be superseded", map[string]any{"fact_id": factID}, nil)
	}
	if oldFact.Status == factStatusSuperseded {
		return nil, errorf(ErrLifecycleInvalid, "superseded fact cannot be superseded again", map[string]any{"fact_id": factID}, nil)
	}
	if input.SpaceID != "" && normalizeSpaceID(input.SpaceID) != oldFact.SpaceID {
		return nil, errorf(ErrLifecycleInvalid, "replacement fact must stay in the same space", map[string]any{"fact_id": factID}, nil)
	}
	if input.SubjectID != oldFact.SubjectID || input.Predicate != oldFact.Predicate {
		return nil, errorf(ErrLifecycleInvalid, "replacement fact must match subject and predicate", map[string]any{"fact_id": factID}, nil)
	}
	spaceID := oldFact.SpaceID
	newFact, err := e.assertFactLocked(spaceID, input, false)
	if err != nil {
		return nil, err
	}
	oldFact.Status = factStatusSuperseded
	if !input.ValidFrom.IsZero() && oldFact.ValidFrom.Before(input.ValidFrom) && (oldFact.ValidTo.IsZero() || input.ValidFrom.Before(oldFact.ValidTo)) {
		oldFact.ValidTo = input.ValidFrom
	}
	oldFact.UpdatedAt = newFact.CreatedAt
	oldFact.Metadata = mergeAnyMap(oldFact.Metadata, map[string]any{
		"superseded_by":    newFact.ID,
		"supersede_reason": reason,
	})
	e.facts[factID] = oldFact
	e.appendFactRevisionLocked(oldFact, "supersede")
	if storedNewFact, ok := e.facts[newFact.ID]; ok {
		storedNewFact.Metadata = mergeAnyMap(storedNewFact.Metadata, map[string]any{
			"supersedes":       factID,
			"supersede_reason": reason,
		})
		e.facts[newFact.ID] = storedNewFact
		e.appendFactRevisionLocked(storedNewFact, "supersede_replacement")
		newFact = cloneFact(storedNewFact)
	}
	return &SupersedeFactResult{
		OldFactID: factID,
		NewFactID: newFact.ID,
	}, nil
}

func (e *engine) RetractFact(ctx context.Context, factID string, reason string) (*RetractFactResult, error) {
	_ = ctx
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
	snapshot := e.snapshotLocked()
	restore := func() {
		e.sequence = snapshot.Sequence
		e.sources = snapshot.Sources
		e.episodes = snapshot.Episodes
		e.entities = snapshot.Entities
		e.facts = snapshot.Facts
		e.factRevisions = snapshot.FactRevisions
		e.entityRevisions = snapshot.EntityRevisions
		e.migrationWatermarks = snapshot.MigrationWatermarks
	}

	fact, ok := e.facts[factID]
	if !ok {
		return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": factID}, nil)
	}
	if fact.Status == factStatusRetracted {
		return &RetractFactResult{
			FactID: fact.ID,
			Status: fact.Status,
		}, nil
	}

	now := e.now()
	fact.Status = factStatusRetracted
	fact.RetractedAt = now
	fact.RetractionReason = reason
	fact.UpdatedAt = now
	e.facts[factID] = fact
	e.appendFactRevisionLocked(fact, "retract")
	if err := e.saveLocked(); err != nil {
		restore()
		return nil, err
	}

	return &RetractFactResult{
		FactID: fact.ID,
		Status: fact.Status,
	}, nil
}

func (e *engine) GetRecord(ctx context.Context, req GetRecordRequest) (*GetRecordResponse, error) {
	_ = ctx
	meta := newQueryMeta(req.Meta.SpaceID, e.now())

	switch req.Kind {
	case "episode":
		record, err := e.GetEpisode(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		if record.SpaceID != meta.SpaceID {
			return nil, errorf(ErrQueryFailed, "record not found in space", map[string]any{"id": req.ID, "space_id": meta.SpaceID}, nil)
		}
		if !e.episodeVisibleAt(*record, req.Temporal) {
			return nil, errorf(ErrQueryFailed, "record not visible at requested time", map[string]any{"id": req.ID}, nil)
		}
		return &GetRecordResponse{Meta: meta, Kind: req.Kind, Record: record}, nil
	case "entity":
		e.mu.RLock()
		entity, ok := e.entities[req.ID]
		if !ok {
			e.mu.RUnlock()
			return nil, errorf(ErrEntityNotFound, "entity not found", map[string]any{"entity_id": req.ID}, nil)
		}
		record := e.entityVersionAt(entity, req.Temporal)
		e.mu.RUnlock()
		if record == nil {
			return nil, errorf(ErrQueryFailed, "record not visible at requested time", map[string]any{"id": req.ID}, nil)
		}
		if record.SpaceID != meta.SpaceID {
			return nil, errorf(ErrQueryFailed, "record not found in space", map[string]any{"id": req.ID, "space_id": meta.SpaceID}, nil)
		}
		return &GetRecordResponse{Meta: meta, Kind: req.Kind, Record: record}, nil
	case "fact":
		record, err := e.GetFact(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		version := record
		if temporalFilterSet(req.Temporal) {
			e.mu.RLock()
			version = e.factVersionAt(*record, req.Temporal)
			e.mu.RUnlock()
		}
		if version == nil {
			return nil, errorf(ErrQueryFailed, "record not visible at requested time", map[string]any{"id": req.ID}, nil)
		}
		if version.SpaceID != meta.SpaceID {
			return nil, errorf(ErrQueryFailed, "record not found in space", map[string]any{"id": req.ID, "space_id": meta.SpaceID}, nil)
		}
		return &GetRecordResponse{Meta: meta, Kind: req.Kind, Record: version}, nil
	case "source":
		record, err := e.GetSource(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		if record.SpaceID != meta.SpaceID {
			return nil, errorf(ErrQueryFailed, "record not found in space", map[string]any{"id": req.ID, "space_id": meta.SpaceID}, nil)
		}
		if !e.sourceVisibleAt(*record, req.Temporal) {
			return nil, errorf(ErrQueryFailed, "record not visible at requested time", map[string]any{"id": req.ID}, nil)
		}
		return &GetRecordResponse{Meta: meta, Kind: req.Kind, Record: record}, nil
	default:
		return nil, errorf(ErrQueryFailed, "unsupported record kind", map[string]any{"kind": req.Kind}, nil)
	}
}

func (e *engine) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	_ = ctx
	query := strings.TrimSpace(strings.ToLower(req.QueryText))
	if query == "" {
		return nil, errorf(ErrInputInvalid, "query_text is required", map[string]any{"field": "query_text"}, nil)
	}

	types := req.Types
	if len(types) == 0 {
		types = []string{"fact", "episode", "entity"}
	}
	mode := req.Mode
	if mode == "" {
		mode = SearchModeHybrid
	}
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)

	hits := make([]SearchHit, 0)
	included := IncludedRecords{}
	graphSeeds := map[string]float64{}
	seenHits := map[string]bool{}
	stats := e.searchCorpusStats(types, req, spaceID)

	if slices.Contains(types, "fact") {
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, append([]string{factRecord.ID, factRecord.SubjectID, factRecord.ObjectID}, factRecord.SupportingEpisodeIDs...)...)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			text := strings.ToLower(factRecord.ValueText + " " + factRecord.Predicate + " " + factRecord.SubjectID + " " + factRecord.ObjectID)
			matched, baseScore, reason := matchSearchWithStats(mode, query, text, stats)
			if matched {
				score := baseScore + 0.4
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if len(req.Predicates) > 0 {
					score += 0.1
					reasons = append(reasons, "predicate_filter")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + factRecord.ID,
					HitType:     "fact",
					RecordID:    factRecord.ID,
					Score:       score,
					MatchedText: factRecord.ValueText,
					Reasons:     reasons,
				})
				seenHits["fact:"+factRecord.ID] = true
				addGraphSeeds(graphSeeds, score, factRecord.ID, factRecord.SubjectID, factRecord.ObjectID)
				addGraphSeeds(graphSeeds, score, factRecord.SupportingEpisodeIDs...)
				included.Facts = append(included.Facts, *factRecord)
				e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
			}
		}
	}
	if slices.Contains(types, "episode") {
		for _, episode := range e.episodes {
			if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) {
				continue
			}
			if len(req.Predicates) > 0 {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			matched, baseScore, reason := matchSearchWithStats(mode, query, strings.ToLower(episode.Content), stats)
			if matched {
				score := baseScore + 0.2
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + episode.ID,
					HitType:     "episode",
					RecordID:    episode.ID,
					Score:       score,
					MatchedText: episode.Content,
					Reasons:     reasons,
				})
				seenHits["episode:"+episode.ID] = true
				addGraphSeeds(graphSeeds, score, episode.ID, episode.SourceID)
				included.Episodes = append(included.Episodes, episode)
				if source, ok := e.sources[episode.SourceID]; ok && source.SpaceID == spaceID && e.sourceVisibleAt(source, req.Temporal) {
					included.Sources = append(included.Sources, source)
				}
			}
		}
	}
	if slices.Contains(types, "entity") {
		for _, entity := range e.entities {
			entityRecord := e.entityVersionAt(entity, req.Temporal)
			if entityRecord == nil || entityRecord.SpaceID != spaceID || !matchesEntityType(*entityRecord, req.Scope.EntityTypes) {
				continue
			}
			if entityMarkedDuplicate(*entityRecord) {
				continue
			}
			if len(req.Predicates) > 0 {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, entityRecord.ID)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			text := strings.ToLower(entityRecord.CanonicalName + " " + strings.Join(entityRecord.Aliases, " "))
			matched, baseScore, reason := matchSearchWithStats(mode, query, text, stats)
			if matched {
				score := baseScore
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + entity.ID,
					HitType:     "entity",
					RecordID:    entityRecord.ID,
					Score:       score,
					MatchedText: entityRecord.CanonicalName,
					Reasons:     reasons,
				})
				seenHits["entity:"+entityRecord.ID] = true
				addGraphSeeds(graphSeeds, score, entityRecord.ID)
				included.Entities = append(included.Entities, *entityRecord)
			}
		}
	}
	if mode != SearchModeKeyword && slices.Contains(types, "fact") && len(graphSeeds) > 0 {
		expandedSeeds := map[string]float64{}
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || seenHits["fact:"+factRecord.ID] || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			score := graphExpansionScore(graphSeeds, *factRecord)
			if score <= 0 {
				continue
			}
			if req.MinScore != nil && score < *req.MinScore {
				continue
			}
			hits = append(hits, SearchHit{
				HitID:       "hit_" + factRecord.ID,
				HitType:     "fact",
				RecordID:    factRecord.ID,
				Score:       score,
				MatchedText: factRecord.ValueText,
				Reasons:     []string{"graph_expansion"},
			})
			seenHits["fact:"+factRecord.ID] = true
			addGraphSeeds(expandedSeeds, score, factRecord.ID, factRecord.SubjectID, factRecord.ObjectID)
			addGraphSeeds(expandedSeeds, score, factRecord.SupportingEpisodeIDs...)
			included.Facts = append(included.Facts, *factRecord)
			e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
		}
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || seenHits["fact:"+factRecord.ID] || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			score := graphExpansionScore(expandedSeeds, *factRecord)
			if score <= 0 {
				continue
			}
			if req.MinScore != nil && score < *req.MinScore {
				continue
			}
			hits = append(hits, SearchHit{
				HitID:       "hit_" + factRecord.ID,
				HitType:     "fact",
				RecordID:    factRecord.ID,
				Score:       score,
				MatchedText: factRecord.ValueText,
				Reasons:     []string{"graph_expansion_bfs"},
			})
			seenHits["fact:"+factRecord.ID] = true
			included.Facts = append(included.Facts, *factRecord)
			e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].RecordID < hits[j].RecordID
		}
		return hits[i].Score > hits[j].Score
	})

	hitsPage, nextCursor, err := paginate(hits, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}

	response := &SearchResponse{
		Meta: newQueryMeta(req.Meta.SpaceID, e.now()),
		Hits: hitsPage,
	}
	response.Meta.NextCursor = nextCursor
	if req.Include.Provenance || req.Include.SupportingEpisodes || req.Include.RelatedEntities || req.Include.Snippets {
		response.Included = dedupeIncluded(included)
	}
	return response, nil
}

func (e *engine) LookupFacts(ctx context.Context, req FactLookupRequest) (*FactLookupResponse, error) {
	_ = ctx
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)

	facts := make([]Fact, 0)
	included := IncludedRecords{}
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, req.Temporal)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
			continue
		}
		if len(req.SubjectIDs) > 0 && !slices.Contains(req.SubjectIDs, factRecord.SubjectID) {
			continue
		}
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
			continue
		}
		if len(req.ObjectIDs) > 0 && !slices.Contains(req.ObjectIDs, factRecord.ObjectID) {
			continue
		}
		if req.ObjectText != "" && !strings.Contains(strings.ToLower(factRecord.ValueText), strings.ToLower(req.ObjectText)) {
			continue
		}
		facts = append(facts, *factRecord)
		included.Facts = append(included.Facts, *factRecord)
		e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
	}

	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].ObservedAt.Equal(facts[j].ObservedAt) {
			return facts[i].ID < facts[j].ID
		}
		return facts[i].ObservedAt.After(facts[j].ObservedAt)
	})
	factPage, nextCursor, err := paginate(facts, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}

	resp := &FactLookupResponse{
		Meta:  newQueryMeta(req.Meta.SpaceID, e.now()),
		Facts: factPage,
	}
	resp.Meta.NextCursor = nextCursor
	if req.Include.Provenance || req.Include.RelatedEntities || req.Include.SupportingEpisodes {
		resp.Included = dedupeIncluded(included)
	}
	return resp, nil
}

func (e *engine) Neighborhood(ctx context.Context, req NeighborhoodRequest) (*NeighborhoodResponse, error) {
	_ = ctx
	if len(req.AnchorIDs) == 0 {
		return nil, errorf(ErrInputInvalid, "anchor_ids is required", map[string]any{"field": "anchor_ids"}, nil)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)

	nodeMap := make(map[string]GraphNode)
	edgeMap := make(map[string]GraphEdge)
	adjacency := make(map[string][]GraphEdge)

	addNode := func(node GraphNode) {
		nodeMap[node.ID] = node
	}
	addEdge := func(edge GraphEdge) {
		edgeMap[edge.ID] = edge
		adjacency[edge.FromID] = append(adjacency[edge.FromID], edge)
		adjacency[edge.ToID] = append(adjacency[edge.ToID], edge)
	}

	for _, entity := range e.entities {
		entityRecord := e.entityVersionAt(entity, req.Temporal)
		if entityRecord == nil || entityRecord.SpaceID != spaceID {
			continue
		}
		addNode(GraphNode{ID: entityRecord.ID, Type: "Entity", Label: entityRecord.CanonicalName})
	}
	for _, source := range e.sources {
		if source.SpaceID != spaceID || !e.sourceVisibleAt(source, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: source.ID, Type: "Source", Label: source.Kind})
	}
	for _, episode := range e.episodes {
		if episode.SpaceID != spaceID || !matchesScopeForEpisode(episode, req.Scope, e.sources) || !e.episodeVisibleAt(episode, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: episode.ID, Type: "Episode", Label: episode.Kind})
		if episode.SourceID != "" {
			addEdge(GraphEdge{ID: "edge:" + episode.ID + ":source", Type: "FROM_SOURCE", FromID: episode.ID, ToID: episode.SourceID})
		}
	}
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, req.Temporal)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate})
		addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":subject", Type: "SUBJECT", FromID: factRecord.ID, ToID: factRecord.SubjectID})
		if factRecord.ObjectID != "" {
			addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":object", Type: "OBJECT", FromID: factRecord.ID, ToID: factRecord.ObjectID})
		}
		for _, episodeID := range factRecord.SupportingEpisodeIDs {
			if _, ok := e.episodes[episodeID]; ok {
				addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":" + episodeID, Type: "ASSERTS", FromID: episodeID, ToID: factRecord.ID})
			}
		}
	}

	maxHops := req.MaxHops
	if maxHops <= 0 {
		maxHops = 1
	}

	dist := make(map[string]int)
	queue := make([]string, 0, len(req.AnchorIDs))
	for _, anchorID := range req.AnchorIDs {
		if _, ok := nodeMap[anchorID]; !ok {
			continue
		}
		dist[anchorID] = 0
		queue = append(queue, anchorID)
	}
	reachableNodes := make(map[string]struct{})
	reachableEdges := make(map[string]struct{})
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		reachableNodes[current] = struct{}{}
		if dist[current] >= maxHops {
			continue
		}
		for _, edge := range adjacency[current] {
			if len(req.EdgeTypes) > 0 && !slices.Contains(req.EdgeTypes, edge.Type) {
				continue
			}
			reachableEdges[edge.ID] = struct{}{}
			nextID := edge.FromID
			if nextID == current {
				nextID = edge.ToID
			}
			if _, seen := dist[nextID]; seen {
				continue
			}
			dist[nextID] = dist[current] + 1
			queue = append(queue, nextID)
		}
	}

	nodes := make([]GraphNode, 0, len(reachableNodes))
	for id := range reachableNodes {
		node := nodeMap[id]
		if len(req.NodeTypes) > 0 && !slices.Contains(req.NodeTypes, node.Type) {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	keptNodes := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		keptNodes[node.ID] = struct{}{}
	}
	edges := make([]GraphEdge, 0, len(reachableEdges))
	for id := range reachableEdges {
		edge := edgeMap[id]
		if _, ok := keptNodes[edge.FromID]; !ok {
			continue
		}
		if _, ok := keptNodes[edge.ToID]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	return &NeighborhoodResponse{
		Meta:  newQueryMeta(req.Meta.SpaceID, e.now()),
		Nodes: limitNodes(nodes, req.MaxNodes),
		Edges: edges,
	}, nil
}

func (e *engine) Timeline(ctx context.Context, req TimelineRequest) (*TimelineResponse, error) {
	_ = ctx
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}
	temporal := req.Temporal
	temporal.IncludeInactive = true
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)

	events := make([]TimelineEvent, 0)
	allowedEvents := req.EventTypes

	addIfAllowed := func(event TimelineEvent) {
		if len(allowedEvents) > 0 && !slices.Contains(allowedEvents, event.EventType) {
			return
		}
		events = append(events, event)
	}

	for _, episode := range e.episodes {
		if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) || !matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID) {
			continue
		}
		event := TimelineEvent{
			EventID:    "evt:" + episode.ID,
			EventType:  "episode",
			RecordType: "episode",
			RecordID:   episode.ID,
			Timestamp:  chooseTime(episode.ObservedAt, episode.IngestedAt),
			Summary:    summarize(episode.Content),
		}
		if !timelineEventVisible(event, temporal) {
			continue
		}
		addIfAllowed(event)
	}
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, temporal)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, temporal) || !matchesAnchors(req.AnchorIDs, append([]string{factRecord.SubjectID, factRecord.ObjectID, factRecord.ID}, factRecord.SupportingEpisodeIDs...)...) {
			continue
		}
		createdEvent := TimelineEvent{
			EventID:    "evt:" + factRecord.ID + ":created",
			EventType:  "fact_created",
			RecordType: "fact",
			RecordID:   factRecord.ID,
			Timestamp:  chooseTime(factRecord.ObservedAt, factRecord.CreatedAt),
			Summary:    factRecord.Predicate,
		}
		if timelineEventVisible(createdEvent, temporal) {
			addIfAllowed(createdEvent)
		}
		if factRecord.Status == factStatusSuperseded {
			event := TimelineEvent{
				EventID:    "evt:" + factRecord.ID + ":superseded",
				EventType:  "fact_superseded",
				RecordType: "fact",
				RecordID:   factRecord.ID,
				Timestamp:  factRecord.UpdatedAt,
				Summary:    factRecord.Predicate,
			}
			if timelineEventVisible(event, temporal) {
				addIfAllowed(event)
			}
		}
		if factRecord.Status == factStatusRetracted {
			event := TimelineEvent{
				EventID:    "evt:" + factRecord.ID + ":retracted",
				EventType:  "fact_retracted",
				RecordType: "fact",
				RecordID:   factRecord.ID,
				Timestamp:  chooseTime(factRecord.RetractedAt, factRecord.UpdatedAt),
				Summary:    factRecord.RetractionReason,
			}
			if timelineEventVisible(event, temporal) {
				addIfAllowed(event)
			}
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].EventID < events[j].EventID
		}
		if req.Descending {
			return events[i].Timestamp.After(events[j].Timestamp)
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	eventPage, nextCursor, err := paginate(events, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}
	return &TimelineResponse{
		Meta: QueryResponseMeta{
			SpaceID:    firstNonEmpty(req.Meta.SpaceID, "default"),
			SnapshotAt: ptrTime(e.now()),
			NextCursor: nextCursor,
		},
		Events: eventPage,
	}, nil
}

func (e *engine) Provenance(ctx context.Context, req ProvenanceRequest) (*ProvenanceResponse, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}

	root := ProvenanceNode{ID: req.ID, Type: strings.Title(req.Kind), Label: req.ID}
	nodes := make([]ProvenanceNode, 0)
	edges := make([]ProvenanceEdge, 0)
	addNode := func(node ProvenanceNode, depth int) bool {
		if depth > maxDepth {
			return false
		}
		nodes = append(nodes, node)
		return true
	}
	addEdge := func(edge ProvenanceEdge, depth int) bool {
		if depth > maxDepth {
			return false
		}
		edges = append(edges, edge)
		return true
	}

	switch req.Kind {
	case "fact":
		fact, ok := e.facts[req.ID]
		factRecord := e.factVersionAt(fact, req.Temporal)
		if !ok || factRecord == nil || factRecord.SpaceID != spaceID {
			return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": req.ID}, nil)
		}
		root.Label = factRecord.Predicate
		root.Meta = factProvenanceMeta(*factRecord)
		nodes = append(nodes, root)
		for _, episodeID := range factRecord.SupportingEpisodeIDs {
			episode, ok := e.episodes[episodeID]
			if !ok || !e.episodeVisibleAt(episode, req.Temporal) {
				continue
			}
			if !addNode(ProvenanceNode{ID: episode.ID, Type: "Episode", Label: episode.Kind}, 1) {
				continue
			}
			addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + fact.ID, Type: "ASSERTS", FromID: episode.ID, ToID: fact.ID}, 1)
			if maxDepth < 2 {
				continue
			}
			if source, ok := e.sources[episode.SourceID]; ok {
				if source.SpaceID != spaceID || !e.sourceVisibleAt(source, req.Temporal) {
					continue
				}
				if addNode(ProvenanceNode{ID: source.ID, Type: "Source", Label: source.Kind}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + source.ID + ":" + episode.ID, Type: "FROM_SOURCE", FromID: episode.ID, ToID: source.ID}, 2)
				}
			}
		}
		if nextID, _ := factRecord.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 1 {
			if nextFact, ok := e.facts[nextID]; ok && nextFact.SpaceID == spaceID {
				nextRecord := e.factVersionAt(nextFact, req.Temporal)
				if nextRecord != nil && addNode(ProvenanceNode{ID: nextRecord.ID, Type: "Fact", Label: nextRecord.Predicate, Meta: factProvenanceMeta(*nextRecord)}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + nextFact.ID + ":" + fact.ID + ":supersedes", Type: "SUPERSEDES", FromID: nextFact.ID, ToID: fact.ID}, 1)
				}
			}
		}
		previousIDs := map[string]bool{}
		for _, previousID := range metadataStringIDs(factRecord.Metadata["supersedes"]) {
			previousIDs[previousID] = true
		}
		for _, candidate := range e.facts {
			if previousID, _ := candidate.Metadata["superseded_by"].(string); previousID == factRecord.ID {
				previousIDs[candidate.ID] = true
			}
		}
		if len(previousIDs) > 0 && maxDepth >= 1 {
			for previousID := range previousIDs {
				if previousFact, ok := e.facts[previousID]; ok && previousFact.SpaceID == spaceID {
					previousRecord := e.factVersionAt(previousFact, req.Temporal)
					if previousRecord != nil && addNode(ProvenanceNode{ID: previousRecord.ID, Type: "Fact", Label: previousRecord.Predicate, Meta: factProvenanceMeta(*previousRecord)}, 1) {
						addEdge(ProvenanceEdge{ID: "prov:" + fact.ID + ":" + previousFact.ID + ":supersedes", Type: "SUPERSEDES", FromID: fact.ID, ToID: previousFact.ID}, 1)
					}
				}
			}
		}
	case "entity":
		entity, ok := e.entities[req.ID]
		entityRecord := e.entityVersionAt(entity, req.Temporal)
		if !ok || entityRecord == nil || entityRecord.SpaceID != spaceID {
			return nil, errorf(ErrEntityNotFound, "entity not found", map[string]any{"entity_id": req.ID}, nil)
		}
		root.Label = entityRecord.CanonicalName
		nodes = append(nodes, root)
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || factRecord.SpaceID != spaceID || (factRecord.SubjectID != entityRecord.ID && factRecord.ObjectID != entityRecord.ID) {
				continue
			}
			if !addNode(ProvenanceNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate}, 1) {
				continue
			}
			edgeType := "SUBJECT"
			if factRecord.ObjectID == entityRecord.ID {
				edgeType = "OBJECT"
			}
			addEdge(ProvenanceEdge{ID: "prov:" + factRecord.ID + ":" + entityRecord.ID, Type: edgeType, FromID: factRecord.ID, ToID: entityRecord.ID}, 1)
			if maxDepth < 2 {
				continue
			}
			for _, episodeID := range factRecord.SupportingEpisodeIDs {
				episode, ok := e.episodes[episodeID]
				if !ok || !e.episodeVisibleAt(episode, req.Temporal) {
					continue
				}
				if addNode(ProvenanceNode{ID: episode.ID, Type: "Episode", Label: episode.Kind}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + factRecord.ID, Type: "ASSERTS", FromID: episode.ID, ToID: factRecord.ID}, 2)
				}
			}
			if nextID, _ := factRecord.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 2 {
				nextFact, ok := e.facts[nextID]
				nextRecord := e.factVersionAt(nextFact, req.Temporal)
				if ok && nextRecord != nil && nextRecord.SpaceID == spaceID && addNode(ProvenanceNode{ID: nextRecord.ID, Type: "Fact", Label: nextRecord.Predicate, Meta: factProvenanceMeta(*nextRecord)}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + nextFact.ID + ":" + fact.ID + ":supersedes", Type: "SUPERSEDES", FromID: nextFact.ID, ToID: fact.ID}, 2)
				}
			}
		}
	case "episode":
		episode, ok := e.episodes[req.ID]
		if !ok || episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) {
			return nil, errorf(ErrQueryFailed, "episode not found", map[string]any{"episode_id": req.ID}, nil)
		}
		root.Label = episode.Kind
		nodes = append(nodes, root)
		if source, ok := e.sources[episode.SourceID]; ok {
			if addNode(ProvenanceNode{ID: source.ID, Type: "Source", Label: source.Kind}, 1) {
				addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + source.ID, Type: "FROM_SOURCE", FromID: episode.ID, ToID: source.ID}, 1)
			}
		}
		if maxDepth >= 1 {
			for _, fact := range e.facts {
				factRecord := e.factVersionAt(fact, req.Temporal)
				if factRecord == nil || factRecord.SpaceID != spaceID || !slices.Contains(factRecord.SupportingEpisodeIDs, episode.ID) {
					continue
				}
				if addNode(ProvenanceNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + factRecord.ID, Type: "ASSERTS", FromID: episode.ID, ToID: factRecord.ID}, 1)
				}
			}
		}
	default:
		return nil, errorf(ErrQueryFailed, "unsupported provenance kind", map[string]any{"kind": req.Kind}, nil)
	}

	return &ProvenanceResponse{
		Meta:  newQueryMeta(req.Meta.SpaceID, e.now()),
		Root:  root,
		Nodes: dedupeProvNodes(nodes),
		Edges: dedupeProvEdges(edges),
	}, nil
}

func (e *engine) GetEpisode(ctx context.Context, id string) (*Episode, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()

	episode, ok := e.episodes[id]
	if !ok {
		return nil, errorf(ErrQueryFailed, "episode not found", map[string]any{"episode_id": id}, nil)
	}
	return cloneEpisode(episode), nil
}

func (e *engine) GetEntity(ctx context.Context, id string) (*Entity, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()

	entity, ok := e.entities[id]
	if !ok {
		return nil, errorf(ErrEntityNotFound, "entity not found", map[string]any{"entity_id": id}, nil)
	}
	return cloneEntity(entity), nil
}

func (e *engine) GetFact(ctx context.Context, id string) (*Fact, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()

	fact, ok := e.facts[id]
	if !ok {
		return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": id}, nil)
	}
	return cloneFact(fact), nil
}

func (e *engine) GetSource(ctx context.Context, id string) (*Source, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()

	source, ok := e.sources[id]
	if !ok {
		return nil, errorf(ErrSourceNotFound, "source not found", map[string]any{"source_id": id}, nil)
	}
	return cloneSource(source), nil
}

func (e *engine) resolveSource(spaceID, sourceID string, input SourceInput, now time.Time) (Source, error) {
	if sourceID == "" && input.ID != "" {
		sourceID = input.ID
	}
	if sourceID == "" {
		if strings.TrimSpace(input.Kind) == "" {
			input.Kind = "inline"
		}
		sourceID = normalizeSourceID(spaceID, input.Kind, input.ExternalRef)
		legacyID := normalizeLegacySourceID(input.Kind, input.ExternalRef)
		if source, ok := e.sources[legacyID]; ok && sourceMatches(source, spaceID, input.Kind, input.ExternalRef) {
			sourceID = legacyID
		}
	}
	if sourceID == "" {
		sourceID = "src:" + normalizeIDPart(spaceID) + ":" + fmt.Sprintf("%d", now.UnixNano())
	}
	sourceSpaceID := normalizeSpaceID(firstNonEmpty(input.SpaceID, spaceID))
	if sourceSpaceID != spaceID {
		return Source{}, errorf(ErrInputInvalid, "source space must match episode space", map[string]any{"source_id": sourceID, "space_id": spaceID}, nil)
	}
	return Source{
		ID:          sourceID,
		SpaceID:     sourceSpaceID,
		Kind:        input.Kind,
		URI:         input.URI,
		ExternalRef: input.ExternalRef,
		Metadata:    cloneAnyMap(input.Metadata),
		CreatedAt:   now,
	}, nil
}

func (e *engine) addFactSupport(included *IncludedRecords, fact Fact, scope ScopeFilter, temporal TemporalFilter) {
	if entity, ok := e.entities[fact.SubjectID]; ok {
		if version := e.entityVersionAt(entity, temporal); version != nil && matchesEntityType(*version, scope.EntityTypes) {
			included.Entities = append(included.Entities, *version)
		}
	}
	if fact.ObjectID != "" {
		if entity, ok := e.entities[fact.ObjectID]; ok {
			if version := e.entityVersionAt(entity, temporal); version != nil && matchesEntityType(*version, scope.EntityTypes) {
				included.Entities = append(included.Entities, *version)
			}
		}
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		if episode, ok := e.episodes[episodeID]; ok && episode.SpaceID == fact.SpaceID && e.episodeVisibleAt(episode, temporal) && matchesScopeForEpisode(episode, scope, e.sources) {
			included.Episodes = append(included.Episodes, episode)
			if source, ok := e.sources[episode.SourceID]; ok && source.SpaceID == fact.SpaceID && source.SpaceID == episode.SpaceID && e.sourceVisibleAt(source, temporal) {
				included.Sources = append(included.Sources, source)
			}
		}
	}
}

func (e *engine) factVisibleAt(fact Fact, filter TemporalFilter) bool {
	if at := asOfTime(filter); at != nil {
		if !factKnownAt(fact, *at) {
			return false
		}
	}
	if !filter.IncludeInactive && fact.Status != factStatusActive {
		return false
	}
	observedAt := chooseTime(fact.ObservedAt, fact.CreatedAt)
	if filter.ObservedFrom != nil && observedAt.Before(*filter.ObservedFrom) {
		return false
	}
	if filter.ObservedTo != nil && observedAt.After(*filter.ObservedTo) {
		return false
	}
	if filter.ValidAt != nil && !factDomainValidAt(fact, *filter.ValidAt) {
		return false
	}
	if !factDomainOverlaps(fact, filter.ValidFrom, filter.ValidTo) {
		return false
	}
	return true
}

func (e *engine) factVersionAt(fact Fact, filter TemporalFilter) *Fact {
	at := asOfTime(filter)
	if at == nil {
		if !e.factVisibleAt(fact, filter) {
			return nil
		}
		return cloneFact(fact)
	}
	revision, ok := e.latestFactRevisionAt(fact.ID, *at)
	if !ok {
		if fact.UpdatedAt.After(*at) {
			return nil
		}
		if !e.factVisibleAt(fact, filter) {
			return nil
		}
		return cloneFact(fact)
	}
	record := revision.toFact()
	if !e.factVisibleAt(record, filter) {
		return nil
	}
	return &record
}

func (e *engine) latestFactRevisionAt(factID string, at time.Time) (FactRevision, bool) {
	var latest FactRevision
	ok := false
	for _, revision := range e.factRevisions {
		if revision.FactID != factID || revision.TxTime.After(at) {
			continue
		}
		if !ok || revision.TxTime.After(latest.TxTime) || (revision.TxTime.Equal(latest.TxTime) && revision.ID > latest.ID) {
			latest = revision
			ok = true
		}
	}
	return latest, ok
}

func (e *engine) episodeVisibleAt(episode Episode, filter TemporalFilter) bool {
	if at := asOfTime(filter); at != nil {
		if episode.IngestedAt.After(*at) {
			return false
		}
	}
	if filter.ObservedFrom != nil && chooseTime(episode.ObservedAt, episode.IngestedAt).Before(*filter.ObservedFrom) {
		return false
	}
	if filter.ObservedTo != nil && chooseTime(episode.ObservedAt, episode.IngestedAt).After(*filter.ObservedTo) {
		return false
	}
	return true
}

func (e *engine) entityVisibleAt(entity Entity, filter TemporalFilter) bool {
	if at := asOfTime(filter); at != nil && entity.CreatedAt.After(*at) {
		return false
	}
	return true
}

func (e *engine) entityVersionAt(entity Entity, filter TemporalFilter) *Entity {
	at := asOfTime(filter)
	if at == nil {
		return cloneEntity(entity)
	}
	if revision, ok := e.latestEntityRevisionAt(entity.ID, *at); ok {
		record := revision.toEntity()
		return &record
	}
	if !e.entityVisibleAt(entity, filter) {
		return nil
	}
	if !entity.UpdatedAt.After(*at) {
		return cloneEntity(entity)
	}

	snapshots := entityHistorySnapshots(entity.Metadata)
	if len(snapshots) == 0 {
		return nil
	}
	asOf := *at
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ChangedAt.Before(snapshots[j].ChangedAt) })
	for _, snapshot := range snapshots {
		if snapshot.ChangedAt.After(asOf) {
			return snapshot.toEntity(entity.ID)
		}
	}
	return cloneEntity(entity)
}

func (e *engine) latestEntityRevisionAt(entityID string, at time.Time) (EntityRevision, bool) {
	var latest EntityRevision
	ok := false
	for _, revision := range e.entityRevisions {
		if revision.EntityID != entityID || revision.TxTime.After(at) {
			continue
		}
		if !ok || revision.TxTime.After(latest.TxTime) || (revision.TxTime.Equal(latest.TxTime) && revision.ID > latest.ID) {
			latest = revision
			ok = true
		}
	}
	return latest, ok
}

func entityMarkedDuplicate(entity Entity) bool {
	if len(entity.Metadata) == 0 {
		return false
	}
	value, ok := entity.Metadata["duplicate_of"]
	if !ok {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) != ""
}

func (e *engine) appendFactRevisionLocked(fact Fact, kind string) {
	revision := newFactRevision(e.newIDLocked("factrev"), fact, kind, chooseTime(fact.UpdatedAt, e.now()))
	e.factRevisions[revision.ID] = revision
}

func (e *engine) appendEntityRevisionLocked(entity Entity, kind string) {
	revision := newEntityRevision(e.newIDLocked("entityrev"), entity, kind, chooseTime(entity.UpdatedAt, e.now()))
	e.entityRevisions[revision.ID] = revision
}

func (e *engine) seedBitemporalRevisionsLocked() {
	if _, ok := e.migrationWatermarks[bitemporalWatermark]; ok {
		return
	}
	appliedAt := e.now()
	for _, fact := range e.facts {
		if fact.Status != factStatusActive && !fact.CreatedAt.IsZero() && fact.UpdatedAt.After(fact.CreatedAt) {
			initial := fact
			initial.Status = factStatusActive
			initial.UpdatedAt = fact.CreatedAt
			initial.RetractedAt = time.Time{}
			initial.RetractionReason = ""
			initial.Metadata = stripFactLifecycleMetadata(initial.Metadata)
			revision := newFactRevision("seed:"+fact.ID+":initial", initial, "migration_seed_initial", fact.CreatedAt)
			if _, ok := e.factRevisions[revision.ID]; !ok {
				e.factRevisions[revision.ID] = revision
			}
		}
		revision := newFactRevision("seed:"+fact.ID+":current", fact, "migration_seed", chooseTime(fact.UpdatedAt, chooseTime(fact.CreatedAt, appliedAt)))
		if _, ok := e.factRevisions[revision.ID]; !ok {
			e.factRevisions[revision.ID] = revision
		}
	}
	for _, entity := range e.entities {
		revision := newEntityRevision("seed:"+entity.ID, entity, "migration_seed", chooseTime(entity.UpdatedAt, chooseTime(entity.CreatedAt, appliedAt)))
		if _, ok := e.entityRevisions[revision.ID]; !ok {
			e.entityRevisions[revision.ID] = revision
		}
	}
	e.migrationWatermarks[bitemporalWatermark] = MigrationWatermark{
		ID:        bitemporalWatermark,
		AppliedAt: appliedAt,
		Metadata: map[string]any{
			"fact_count":   len(e.facts),
			"entity_count": len(e.entities),
		},
	}
}

func stripFactLifecycleMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := cloneAnyMap(src)
	for key := range reservedFactMetadataKeys {
		delete(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func newFactRevision(id string, fact Fact, kind string, txTime time.Time) FactRevision {
	if txTime.IsZero() {
		txTime = fact.CreatedAt
	}
	return FactRevision{
		ID:                   id,
		FactID:               fact.ID,
		SpaceID:              fact.SpaceID,
		RevisionKind:         kind,
		TxTime:               txTime.UTC(),
		Predicate:            fact.Predicate,
		SubjectID:            fact.SubjectID,
		ObjectID:             fact.ObjectID,
		ValueText:            fact.ValueText,
		Confidence:           fact.Confidence,
		Status:               fact.Status,
		ValidFrom:            fact.ValidFrom,
		ValidTo:              fact.ValidTo,
		ObservedAt:           fact.ObservedAt,
		CreatedAt:            fact.CreatedAt,
		UpdatedAt:            fact.UpdatedAt,
		RetractedAt:          fact.RetractedAt,
		RetractionReason:     fact.RetractionReason,
		SupportingEpisodeIDs: slices.Clone(fact.SupportingEpisodeIDs),
		Metadata:             cloneAnyMap(fact.Metadata),
	}
}

func (r FactRevision) toFact() Fact {
	return Fact{
		ID:                   r.FactID,
		SpaceID:              r.SpaceID,
		Predicate:            r.Predicate,
		SubjectID:            r.SubjectID,
		ObjectID:             r.ObjectID,
		ValueText:            r.ValueText,
		Confidence:           r.Confidence,
		Status:               r.Status,
		ValidFrom:            r.ValidFrom,
		ValidTo:              r.ValidTo,
		ObservedAt:           r.ObservedAt,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
		RetractedAt:          r.RetractedAt,
		RetractionReason:     r.RetractionReason,
		SupportingEpisodeIDs: slices.Clone(r.SupportingEpisodeIDs),
		Metadata:             cloneAnyMap(r.Metadata),
	}
}

func newEntityRevision(id string, entity Entity, kind string, txTime time.Time) EntityRevision {
	if txTime.IsZero() {
		txTime = entity.CreatedAt
	}
	return EntityRevision{
		ID:            id,
		EntityID:      entity.ID,
		SpaceID:       entity.SpaceID,
		RevisionKind:  kind,
		TxTime:        txTime.UTC(),
		Namespace:     entity.Namespace,
		Type:          entity.Type,
		CanonicalName: entity.CanonicalName,
		Aliases:       slices.Clone(entity.Aliases),
		Metadata:      metadataWithoutEntityHistory(entity.Metadata),
		CreatedAt:     entity.CreatedAt,
		UpdatedAt:     entity.UpdatedAt,
	}
}

func (r EntityRevision) toEntity() Entity {
	return Entity{
		ID:            r.EntityID,
		SpaceID:       r.SpaceID,
		Namespace:     r.Namespace,
		Type:          r.Type,
		CanonicalName: r.CanonicalName,
		Aliases:       slices.Clone(r.Aliases),
		Metadata:      cloneAnyMap(r.Metadata),
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func factProvenanceMeta(fact Fact) map[string]any {
	meta := map[string]any{
		"status": fact.Status,
	}
	if !fact.ValidFrom.IsZero() {
		meta["valid_from"] = fact.ValidFrom
	}
	if !fact.ValidTo.IsZero() {
		meta["valid_to"] = fact.ValidTo
	}
	if !fact.RetractedAt.IsZero() {
		meta["retracted_at"] = fact.RetractedAt
	}
	if fact.RetractionReason != "" {
		meta["retraction_reason"] = fact.RetractionReason
	}
	if supersedes := metadataStringIDs(fact.Metadata["supersedes"]); len(supersedes) == 1 {
		meta["supersedes"] = supersedes[0]
	} else if len(supersedes) > 1 {
		meta["supersedes"] = supersedes
	}
	if supersededBy, _ := fact.Metadata["superseded_by"].(string); supersededBy != "" {
		meta["superseded_by"] = supersededBy
	}
	if reason, _ := fact.Metadata["supersede_reason"].(string); reason != "" {
		meta["supersede_reason"] = reason
	}
	return meta
}

func metadataStringIDs(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	case []string:
		return dedupeStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return dedupeStrings(out)
	default:
		return nil
	}
}

type entityHistorySnapshot struct {
	ChangedAt     time.Time
	SpaceID       string
	Namespace     string
	Type          string
	CanonicalName string
	Aliases       []string
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func entitySnapshotEntry(entity Entity, changedAt time.Time) map[string]any {
	return map[string]any{
		"changed_at":     changedAt.UTC().Format(time.RFC3339Nano),
		"space_id":       entity.SpaceID,
		"namespace":      entity.Namespace,
		"type":           entity.Type,
		"canonical_name": entity.CanonicalName,
		"aliases":        slices.Clone(entity.Aliases),
		"metadata":       metadataWithoutEntityHistory(entity.Metadata),
		"created_at":     entity.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":     entity.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func appendEntityHistory(metadata map[string]any, entry map[string]any) map[string]any {
	out := metadataWithoutEntityHistory(metadata)
	history := make([]any, 0, 1)
	if metadata != nil {
		if raw, ok := metadata[entityHistoryKey].([]any); ok {
			history = append(history, raw...)
		}
	}
	history = append(history, entry)
	if out == nil {
		out = make(map[string]any, 1)
	}
	out[entityHistoryKey] = history
	return out
}

func metadataWithoutEntityHistory(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := cloneAnyMap(metadata)
	delete(out, entityHistoryKey)
	if len(out) == 0 {
		return nil
	}
	return out
}

func entityHistorySnapshots(metadata map[string]any) []entityHistorySnapshot {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[entityHistoryKey].([]any)
	if !ok {
		return nil
	}
	out := make([]entityHistorySnapshot, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, entityHistorySnapshot{
			ChangedAt:     parseMaybeTime(entry["changed_at"]),
			SpaceID:       fmt.Sprint(entry["space_id"]),
			Namespace:     fmt.Sprint(entry["namespace"]),
			Type:          fmt.Sprint(entry["type"]),
			CanonicalName: fmt.Sprint(entry["canonical_name"]),
			Aliases:       anyStringsFromMetadata(entry["aliases"]),
			Metadata:      anyMapFromMetadata(entry["metadata"]),
			CreatedAt:     parseMaybeTime(entry["created_at"]),
			UpdatedAt:     parseMaybeTime(entry["updated_at"]),
		})
	}
	return out
}

func (s entityHistorySnapshot) toEntity(id string) *Entity {
	return &Entity{
		ID:            id,
		SpaceID:       s.SpaceID,
		Namespace:     s.Namespace,
		Type:          s.Type,
		CanonicalName: s.CanonicalName,
		Aliases:       slices.Clone(s.Aliases),
		Metadata:      cloneAnyMap(s.Metadata),
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func anyStringsFromMetadata(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func anyMapFromMetadata(value any) map[string]any {
	entry, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return cloneAnyMap(entry)
}

func parseMaybeTime(value any) time.Time {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" || raw == "<nil>" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (e *engine) sourceVisibleAt(source Source, filter TemporalFilter) bool {
	if filter.AsOf != nil && source.CreatedAt.After(*filter.AsOf) {
		return false
	}
	return true
}

func (e *engine) matchesScopeForFact(fact Fact, scope ScopeFilter, filter TemporalFilter) bool {
	if len(scope.FactStatus) > 0 && !slices.Contains(scope.FactStatus, fact.Status) {
		return false
	}
	if len(scope.EntityTypes) > 0 {
		subject, subjectOK := e.entities[fact.SubjectID]
		object, objectOK := e.entities[fact.ObjectID]
		if subjectOK {
			subjectOK = false
			if version := e.entityVersionAt(subject, filter); version != nil {
				subject = *version
				subjectOK = true
			}
		}
		if objectOK {
			objectOK = false
			if version := e.entityVersionAt(object, filter); version != nil {
				object = *version
				objectOK = true
			}
		}
		if (!subjectOK || !matchesEntityType(subject, scope.EntityTypes)) && (!objectOK || !matchesEntityType(object, scope.EntityTypes)) {
			return false
		}
	}
	if len(scope.GroupIDs) == 0 && len(scope.SourceIDs) == 0 && len(scope.SourceKinds) == 0 {
		return true
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		if episode, ok := e.episodes[episodeID]; ok && e.episodeVisibleAt(episode, filter) && matchesScopeForEpisode(episode, scope, e.sources) {
			return true
		}
	}
	return false
}

func matchesScopeForEpisode(episode Episode, scope ScopeFilter, sources map[string]Source) bool {
	source, ok := sources[episode.SourceID]
	if !ok || source.SpaceID != episode.SpaceID {
		return false
	}
	if len(scope.GroupIDs) > 0 && !slices.Contains(scope.GroupIDs, episode.GroupID) {
		return false
	}
	if len(scope.SourceIDs) > 0 && !slices.Contains(scope.SourceIDs, episode.SourceID) {
		return false
	}
	if len(scope.SourceKinds) > 0 {
		if !slices.Contains(scope.SourceKinds, source.Kind) {
			return false
		}
	}
	return true
}

// RecordPassesSearchFilters applies Yeoul's canonical search post-filter to records
// returned by derived indexes.
func RecordPassesSearchFilters(ctx context.Context, eng Engine, record any, req SearchRequest) bool {
	switch value := record.(type) {
	case *Fact:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "fact", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Fact)
		if value == nil {
			return false
		}
		if !req.Temporal.IncludeInactive && value.Status != factStatusActive {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, append([]string{value.ID, value.SubjectID, value.ObjectID}, value.SupportingEpisodeIDs...)...) {
			return false
		}
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, value.Predicate) {
			return false
		}
		if len(req.Scope.FactStatus) > 0 && !slices.Contains(req.Scope.FactStatus, value.Status) {
			return false
		}
		if len(req.Scope.EntityTypes) > 0 {
			subject, subjectErr := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.SubjectID, Temporal: req.Temporal})
			object, objectErr := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.ObjectID, Temporal: req.Temporal})
			var subjectEntity *Entity
			var objectEntity *Entity
			if subjectErr == nil {
				subjectEntity, _ = subject.Record.(*Entity)
			}
			if objectErr == nil {
				objectEntity, _ = object.Record.(*Entity)
			}
			if (subjectEntity == nil || !matchesEntityType(*subjectEntity, req.Scope.EntityTypes)) && (objectEntity == nil || !matchesEntityType(*objectEntity, req.Scope.EntityTypes)) {
				return false
			}
		}
		if len(req.Scope.GroupIDs) == 0 && len(req.Scope.SourceIDs) == 0 && len(req.Scope.SourceKinds) == 0 {
			return true
		}
		for _, episodeID := range value.SupportingEpisodeIDs {
			episode, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "episode", ID: episodeID, Temporal: req.Temporal})
			if err == nil && RecordPassesSearchFilters(ctx, eng, episode.Record, SearchRequest{Meta: req.Meta, Scope: req.Scope, Temporal: req.Temporal}) {
				return true
			}
		}
		return false
	case *Episode:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "episode", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Episode)
		if value == nil {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, value.ID, value.SourceID) {
			return false
		}
		if len(req.Predicates) > 0 {
			return false
		}
		if len(req.Scope.GroupIDs) > 0 && !slices.Contains(req.Scope.GroupIDs, value.GroupID) {
			return false
		}
		if len(req.Scope.SourceIDs) > 0 && !slices.Contains(req.Scope.SourceIDs, value.SourceID) {
			return false
		}
		if len(req.Scope.SourceKinds) > 0 {
			source, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "source", ID: value.SourceID, Temporal: req.Temporal})
			if err != nil || source == nil {
				return false
			}
			sourceRecord, _ := source.Record.(*Source)
			if sourceRecord == nil || sourceRecord.SpaceID != value.SpaceID || !slices.Contains(req.Scope.SourceKinds, sourceRecord.Kind) {
				return false
			}
		}
		return true
	case *Entity:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Entity)
		if value == nil {
			return false
		}
		if entityMarkedDuplicate(*value) {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, value.ID) {
			return false
		}
		return len(req.Predicates) == 0 && matchesEntityType(*value, req.Scope.EntityTypes)
	default:
		return false
	}
}

func matchesEntityType(entity Entity, types []string) bool {
	return len(types) == 0 || slices.Contains(types, entity.Type)
}

func validFactStatus(status string) bool {
	switch status {
	case factStatusActive, factStatusSuperseded, factStatusRetracted:
		return true
	default:
		return false
	}
}

func matchesAnchors(anchors []string, ids ...string) bool {
	if len(anchors) == 0 {
		return true
	}
	for _, id := range ids {
		if id != "" && slices.Contains(anchors, id) {
			return true
		}
	}
	return false
}

func addGraphSeeds(seeds map[string]float64, score float64, ids ...string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seeds[id] >= score {
			continue
		}
		seeds[id] = score
	}
}

func graphExpansionScore(seeds map[string]float64, fact Fact) float64 {
	best := 0.0
	for _, id := range append([]string{fact.SubjectID, fact.ObjectID}, fact.SupportingEpisodeIDs...) {
		if score := seeds[id] * 0.45; score > best {
			best = score
		}
	}
	return best
}

func dedupeIncluded(in IncludedRecords) IncludedRecords {
	return IncludedRecords{
		Episodes: dedupeEpisodes(in.Episodes),
		Entities: dedupeEntities(in.Entities),
		Facts:    dedupeFacts(in.Facts),
		Sources:  dedupeSources(in.Sources),
	}
}

func dedupeEpisodes(items []Episode) []Episode {
	seen := make(map[string]Episode)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Episode, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeEntities(items []Entity) []Entity {
	seen := make(map[string]Entity)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Entity, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeFacts(items []Fact) []Fact {
	seen := make(map[string]Fact)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Fact, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeSources(items []Source) []Source {
	seen := make(map[string]Source)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Source, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeProvNodes(items []ProvenanceNode) []ProvenanceNode {
	seen := make(map[string]ProvenanceNode)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]ProvenanceNode, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeProvEdges(items []ProvenanceEdge) []ProvenanceEdge {
	seen := make(map[string]ProvenanceEdge)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]ProvenanceEdge, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func limitNodes(nodes []GraphNode, max int) []GraphNode {
	if max <= 0 || max >= len(nodes) {
		return nodes
	}
	return nodes[:max]
}

func newQueryMeta(spaceID string, now time.Time) QueryResponseMeta {
	return QueryResponseMeta{
		SpaceID:    firstNonEmpty(spaceID, "default"),
		SnapshotAt: &now,
	}
}

func pageLimit(limit, total int) int {
	if limit <= 0 || limit > total {
		return total
	}
	return limit
}

func paginate[T any](items []T, offset, limit int) ([]T, string, error) {
	if offset < 0 {
		return nil, "", errorf(ErrQueryFailed, "cursor offset is invalid", nil, nil)
	}
	if offset > len(items) {
		return []T{}, "", nil
	}
	pageSize := pageLimit(limit, len(items)-offset)
	if pageSize < 0 {
		pageSize = 0
	}
	end := offset + pageSize
	page := items[offset:end]
	if end >= len(items) {
		return page, "", nil
	}
	return page, encodeCursor(end), nil
}

func decodeCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	if !strings.HasPrefix(cursor, "offset:") {
		return 0, errorf(ErrQueryFailed, "cursor_invalid", map[string]any{"cursor": cursor}, nil)
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, "offset:"))
	if err != nil || offset < 0 {
		return 0, errorf(ErrQueryFailed, "cursor_invalid", map[string]any{"cursor": cursor}, nil)
	}
	return offset, nil
}

func encodeCursor(offset int) string {
	return fmt.Sprintf("offset:%d", offset)
}

type sparseCorpusStats struct {
	DocCount int
	DF       map[string]int
}

func (e *engine) searchCorpusStats(types []string, req SearchRequest, spaceID string) *sparseCorpusStats {
	stats := &sparseCorpusStats{DF: map[string]int{}}
	observe := func(text string) {
		tokens := tokenize(text)
		if len(tokens) == 0 {
			return
		}
		stats.DocCount++
		for _, token := range tokens {
			stats.DF[token]++
		}
	}
	if slices.Contains(types, "fact") {
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, append([]string{factRecord.ID, factRecord.SubjectID, factRecord.ObjectID}, factRecord.SupportingEpisodeIDs...)...) {
				continue
			}
			observe(factRecord.ValueText + " " + factRecord.Predicate + " " + factRecord.SubjectID + " " + factRecord.ObjectID)
		}
	}
	if slices.Contains(types, "episode") && len(req.Predicates) == 0 {
		for _, episode := range e.episodes {
			if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID) {
				continue
			}
			observe(episode.Content)
		}
	}
	if slices.Contains(types, "entity") && len(req.Predicates) == 0 {
		for _, entity := range e.entities {
			entityRecord := e.entityVersionAt(entity, req.Temporal)
			if entityRecord == nil || entityRecord.SpaceID != spaceID || !matchesEntityType(*entityRecord, req.Scope.EntityTypes) || entityMarkedDuplicate(*entityRecord) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, entityRecord.ID) {
				continue
			}
			observe(entityRecord.CanonicalName + " " + strings.Join(entityRecord.Aliases, " "))
		}
	}
	if stats.DocCount == 0 {
		return nil
	}
	return stats
}

func asOfTime(filter TemporalFilter) *time.Time {
	return filter.AsOf
}

func temporalFilterSet(filter TemporalFilter) bool {
	return filter.AsOf != nil ||
		filter.ValidAt != nil ||
		filter.ObservedFrom != nil ||
		filter.ObservedTo != nil ||
		filter.ValidFrom != nil ||
		filter.ValidTo != nil ||
		filter.IncludeInactive
}

func factKnownAt(fact Fact, at time.Time) bool {
	return !fact.CreatedAt.After(at)
}

func factDomainValidAt(fact Fact, at time.Time) bool {
	if !fact.ValidFrom.IsZero() && fact.ValidFrom.After(at) {
		return false
	}
	if !fact.ValidTo.IsZero() && !at.Before(fact.ValidTo) {
		return false
	}
	return true
}

func validFactInterval(from, to time.Time) bool {
	return from.IsZero() || to.IsZero() || from.Before(to)
}

func factDomainOverlaps(fact Fact, from, to *time.Time) bool {
	if from != nil && !fact.ValidTo.IsZero() && !fact.ValidTo.After(*from) {
		return false
	}
	if to != nil && !fact.ValidFrom.IsZero() && !fact.ValidFrom.Before(*to) {
		return false
	}
	return true
}

func factValidityOverlaps(aFrom, aTo, bFrom, bTo time.Time) bool {
	if !aTo.IsZero() && !bFrom.IsZero() && !aTo.After(bFrom) {
		return false
	}
	if !bTo.IsZero() && !aFrom.IsZero() && !bTo.After(aFrom) {
		return false
	}
	return true
}

func timelineEventVisible(event TimelineEvent, filter TemporalFilter) bool {
	if at := asOfTime(filter); at != nil && event.Timestamp.After(*at) {
		return false
	}
	if filter.ObservedFrom != nil && event.Timestamp.Before(*filter.ObservedFrom) {
		return false
	}
	if filter.ObservedTo != nil && event.Timestamp.After(*filter.ObservedTo) {
		return false
	}
	return true
}

func matchSearch(mode SearchMode, query, text string) (bool, float64, string) {
	return matchSearchWithStats(mode, query, text, nil)
}

func matchSearchWithStats(mode SearchMode, query, text string, stats *sparseCorpusStats) (bool, float64, string) {
	query = strings.TrimSpace(strings.ToLower(query))
	text = strings.TrimSpace(strings.ToLower(text))
	if query == "" || text == "" {
		return false, 0, ""
	}
	if strings.Contains(text, query) {
		switch mode {
		case SearchModeKeyword:
			return true, 1.0, "keyword_match"
		case SearchModeSemantic:
			return true, 0.9, "semantic_fallback"
		default:
			return true, 1.0, "hybrid_keyword_match"
		}
	}

	score := sparseTermScore(query, text, stats)
	switch mode {
	case SearchModeKeyword:
		return false, 0, ""
	case SearchModeSemantic:
		if score <= 0 {
			vectorScore := charNgramCosine(query, text)
			if vectorScore < 0.25 {
				return false, 0, ""
			}
			return true, 0.35 + vectorScore*0.4, "semantic_vector_similarity"
		}
		return true, 0.5 + score*0.5, "semantic_token_overlap"
	default:
		if score <= 0 {
			vectorScore := charNgramCosine(query, text)
			if vectorScore < 0.25 {
				return false, 0, ""
			}
			return true, 0.25 + vectorScore*0.35, "hybrid_vector_similarity"
		}
		return true, 0.35 + score*0.45, "hybrid_token_overlap"
	}
}

func charNgramCosine(query, text string) float64 {
	queryVector := charNgramVector(query, 3)
	textVector := charNgramVector(text, 3)
	if len(queryVector) == 0 || len(textVector) == 0 {
		return 0
	}
	dot := 0.0
	queryNorm := 0.0
	textNorm := 0.0
	for gram, count := range queryVector {
		queryNorm += count * count
		dot += count * textVector[gram]
	}
	for _, count := range textVector {
		textNorm += count * count
	}
	if queryNorm == 0 || textNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(queryNorm) * math.Sqrt(textNorm))
}

func charNgramVector(value string, n int) map[string]float64 {
	value = strings.Join(strings.Fields(strings.ToLower(value)), " ")
	runes := []rune(value)
	if len(runes) < n {
		return nil
	}
	out := map[string]float64{}
	for i := 0; i <= len(runes)-n; i++ {
		out[string(runes[i:i+n])]++
	}
	return out
}

func sparseTermScore(query, text string, stats *sparseCorpusStats) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	textTerms := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '-' || r == '_' || r == ':' || r == ',' || r == '.'
	})
	if len(textTerms) == 0 {
		return 0
	}
	tf := make(map[string]int)
	for _, token := range textTerms {
		token = strings.TrimSpace(token)
		if token != "" {
			tf[token]++
		}
	}
	score := 0.0
	for _, token := range queryTokens {
		count := tf[token]
		if count == 0 {
			continue
		}
		idf := 1.0
		if stats != nil && stats.DocCount > 0 {
			df := stats.DF[token]
			idf = math.Log(1 + (float64(stats.DocCount)-float64(df)+0.5)/(float64(df)+0.5))
		}
		score += idf * float64(count) / (float64(count) + 1.2 + 0.25*float64(len(textTerms)))
	}
	score = score / float64(len(queryTokens))
	if score > 1 {
		return 1
	}
	return score
}

func tokenize(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '-' || r == '_' || r == ':' || r == ',' || r == '.'
	})
	return dedupeStrings(parts)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func chooseTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	return fallback
}

func summarize(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 80 {
		return content
	}
	return content[:77] + "..."
}

func normalizeEntityID(namespace, entityType, canonical string) string {
	return EntityID(namespace, entityType, canonical)
}

func normalizeSourceID(spaceID, kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(spaceID), normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

func normalizeLegacySourceID(kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

func sourceMatches(source Source, spaceID, kind, externalRef string) bool {
	return source.SpaceID == spaceID && source.Kind == kind && source.ExternalRef == externalRef
}

func normalizeSpaceID(spaceID string) string {
	return firstNonEmpty(spaceID, "default")
}

func normalizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	return value
}

func (e *engine) newIDLocked(prefix string) string {
	for {
		next := atomic.AddUint64(&e.sequence, 1)
		id := fmt.Sprintf("%s_%06d", prefix, next)
		if !e.idExistsLocked(id) {
			return id
		}
	}
}

func (e *engine) idExistsLocked(id string) bool {
	if _, ok := e.sources[id]; ok {
		return true
	}
	if _, ok := e.episodes[id]; ok {
		return true
	}
	if _, ok := e.entities[id]; ok {
		return true
	}
	if _, ok := e.facts[id]; ok {
		return true
	}
	if _, ok := e.factRevisions[id]; ok {
		return true
	}
	if _, ok := e.entityRevisions[id]; ok {
		return true
	}
	if _, ok := e.migrationWatermarks[id]; ok {
		return true
	}
	return false
}

func (e *engine) applyState(state persistedState) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sequence = state.Sequence
	e.sources = defaultSources(state.Sources)
	e.episodes = defaultEpisodes(state.Episodes)
	e.entities = defaultEntities(state.Entities)
	e.facts = defaultFacts(state.Facts)
	e.factRevisions = defaultFactRevisions(state.FactRevisions)
	e.entityRevisions = defaultEntityRevisions(state.EntityRevisions)
	e.migrationWatermarks = defaultMigrationWatermarks(state.MigrationWatermarks)
	e.seedBitemporalRevisionsLocked()
}

func (e *engine) snapshotLocked() persistedState {
	return clonePersistedState(persistedState{
		Version:             1,
		Sequence:            e.sequence,
		Sources:             e.sources,
		Episodes:            e.episodes,
		Entities:            e.entities,
		Facts:               e.facts,
		FactRevisions:       e.factRevisions,
		EntityRevisions:     e.entityRevisions,
		MigrationWatermarks: e.migrationWatermarks,
	})
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneAny(v)
	}
	return dst
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneAny(v[i])
		}
		return out
	case []string:
		return slices.Clone(v)
	default:
		return v
	}
}

func mergeAnyMap(base, incoming map[string]any) map[string]any {
	if len(base) == 0 && len(incoming) == 0 {
		return nil
	}
	out := cloneAnyMap(base)
	if out == nil {
		out = make(map[string]any, len(incoming))
	}
	for k, v := range incoming {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneEpisode(src Episode) *Episode {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneEntity(src Entity) *Entity {
	dst := src
	dst.Aliases = slices.Clone(src.Aliases)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneFact(src Fact) *Fact {
	dst := src
	dst.SupportingEpisodeIDs = slices.Clone(src.SupportingEpisodeIDs)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneFactRevision(src FactRevision) *FactRevision {
	dst := src
	dst.SupportingEpisodeIDs = slices.Clone(src.SupportingEpisodeIDs)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneEntityRevision(src EntityRevision) *EntityRevision {
	dst := src
	dst.Aliases = slices.Clone(src.Aliases)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneMigrationWatermark(src MigrationWatermark) *MigrationWatermark {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneSource(src Source) *Source {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func (e *engine) saveLocked() error {
	if e.store == nil {
		return nil
	}
	return e.store.Save(e.snapshotLocked())
}
