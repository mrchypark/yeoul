package yeoul

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	var errs []error
	if err := e.saveLocked(); err != nil {
		errs = append(errs, err)
	}
	if e.store != nil {
		if err := e.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *engine) Snapshot(ctx context.Context) (*DatabaseSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	state := e.snapshotLocked()
	return &DatabaseSnapshot{
		Version:             state.Version,
		Sequence:            state.Sequence,
		Sources:             state.Sources,
		Episodes:            state.Episodes,
		Entities:            state.Entities,
		Facts:               state.Facts,
		FactRevisions:       state.FactRevisions,
		EntityRevisions:     state.EntityRevisions,
		MigrationWatermarks: state.MigrationWatermarks,
	}, nil
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

	var result *EpisodeResult
	err := e.mutateLocked(func() error {
		source, err := e.resolveSource(spaceID, input.SourceID, input.Source, now)
		if err != nil {
			return err
		}
		result, err = e.ingestEpisodeLocked(spaceID, input, source, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *engine) IngestBatch(ctx context.Context, input BatchInput) (*BatchResult, error) {
	_ = ctx
	result := &BatchResult{}
	err := e.mutateLocked(func() error {
		for _, episode := range input.Episodes {
			if strings.TrimSpace(episode.Kind) == "" || strings.TrimSpace(episode.Content) == "" {
				return errorf(ErrInputInvalid, "episode kind and content are required", map[string]any{"kind": episode.Kind, "id": episode.ID}, nil)
			}
			now := e.now()
			spaceID := normalizeSpaceID(episode.SpaceID)
			source, err := e.resolveSource(spaceID, episode.SourceID, episode.Source, now)
			if err != nil {
				return err
			}
			item, err := e.ingestEpisodeLocked(spaceID, episode, source, now)
			if err != nil {
				return err
			}
			result.EpisodeIDs = append(result.EpisodeIDs, item.EpisodeID)
		}
		referenceRewrites := make(map[string]string)
		for _, entity := range input.Entities {
			if strings.TrimSpace(entity.Type) == "" || strings.TrimSpace(entity.CanonicalName) == "" {
				return errorf(ErrInputInvalid, "entity type and canonical_name are required", map[string]any{"id": entity.ID}, nil)
			}
			item, err := e.upsertEntityLocked(entity)
			if err != nil {
				return err
			}
			result.EntityIDs = append(result.EntityIDs, item.ID)
			// Register explicit IDs verbatim: trimming them here would
			// redirect references meant for a different entity.
			reference := entity.ID
			if reference == "" {
				reference = EntityID(entity.Namespace, entity.Type, firstNonEmpty(entity.StableKey, entity.CanonicalName))
			}
			if existing, ok := referenceRewrites[reference]; ok && existing != item.ID {
				return errorf(ErrLifecycleInvalid, "ambiguous entity reference in batch", map[string]any{
					"reference":   reference,
					"entity_id":   existing,
					"conflict_id": item.ID,
				}, nil)
			}
			referenceRewrites[reference] = item.ID
		}
		for _, fact := range input.Facts {
			if strings.TrimSpace(fact.Predicate) == "" || strings.TrimSpace(fact.SubjectID) == "" || len(fact.SupportingEpisodeIDs) == 0 {
				return errorf(ErrInputInvalid, "fact predicate, subject_id, and supporting_episode_ids are required", map[string]any{"id": fact.ID}, nil)
			}
			if rewritten, ok := referenceRewrites[fact.SubjectID]; ok {
				fact.SubjectID = rewritten
			}
			if rewritten, ok := referenceRewrites[fact.ObjectID]; ok {
				fact.ObjectID = rewritten
			}
			item, err := e.assertFactLocked(normalizeSpaceID(fact.SpaceID), fact, false)
			if err != nil {
				return err
			}
			result.FactIDs = append(result.FactIDs, item.ID)
		}
		if len(input.EntityRevisions) > 0 || len(input.FactRevisions) > 0 {
			return errorf(ErrLifecycleInvalid, "revision import is not supported by public batch ingest", nil, nil)
		}
		return nil
	})
	if err != nil {
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

	var entity *Entity
	err := e.mutateLocked(func() error {
		var err error
		entity, err = e.upsertEntityLocked(input)
		return err
	})
	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (e *engine) ingestEpisodeLocked(spaceID string, input EpisodeInput, source Source, now time.Time) (*EpisodeResult, error) {
	if existing, ok := e.sources[source.ID]; ok {
		if existing.SpaceID != spaceID {
			return nil, errorf(ErrLifecycleInvalid, "source id already exists in another space", map[string]any{"source_id": source.ID, "space_id": spaceID}, nil)
		}
		if input.SourceID == "" && input.Source.ID == "" && (existing.SpaceID != source.SpaceID || existing.Kind != source.Kind || existing.ExternalRef != source.ExternalRef) {
			return nil, errorf(ErrLifecycleInvalid, "source id already exists with different identity", map[string]any{"source_id": source.ID}, nil)
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
	derived := id == ""
	if derived {
		identity := firstNonEmpty(input.StableKey, input.CanonicalName)
		id = EntityID(input.Namespace, input.Type, identity)
		if _, ok := e.entities[id]; !ok {
			if legacyID := legacyEntityID(input.Namespace, input.Type, identity); legacyID != id {
				if legacy, ok := e.entities[legacyID]; ok && legacyEntityIdentityMatches(legacy, input) {
					id = legacyID
				}
			}
		}
	}
	metadata := cloneAnyMap(input.Metadata)
	if metadata != nil {
		// stable_key is engine-managed: ordinary metadata must not be able to
		// change a stored strong identity.
		delete(metadata, "stable_key")
	}
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

	if derived && !entityIdentityMatches(entity, input) {
		return nil, errorf(ErrLifecycleInvalid, "entity id already exists with different identity", map[string]any{
			"entity_id": id,
		}, nil)
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

	spaceID := normalizeSpaceID(input.SpaceID)
	var fact *Fact
	err := e.mutateLocked(func() error {
		var err error
		fact, err = e.assertFactLocked(spaceID, input, false)
		return err
	})
	if err != nil {
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
	// Supporting episode IDs are stored verbatim, so validation must run against
	// that exact representation. Rejecting non-canonical IDs keeps a reference
	// from silently retargeting another episode or collapsing to an empty list.
	supportingEpisodeIDs := make([]string, 0, len(input.SupportingEpisodeIDs))
	seenEpisodeIDs := make(map[string]struct{}, len(input.SupportingEpisodeIDs))
	for _, episodeID := range input.SupportingEpisodeIDs {
		if episodeID == "" || strings.TrimSpace(episodeID) != episodeID {
			return nil, errorf(ErrInputInvalid, "supporting episode id must not be empty or padded with whitespace", map[string]any{"episode_id": episodeID}, nil)
		}
		if _, ok := seenEpisodeIDs[episodeID]; ok {
			continue
		}
		seenEpisodeIDs[episodeID] = struct{}{}
		episode, ok := e.episodes[episodeID]
		if !ok {
			return nil, errorf(ErrInputInvalid, "supporting episode not found", map[string]any{"episode_id": episodeID}, nil)
		}
		if episode.SpaceID != spaceID {
			return nil, errorf(ErrInputInvalid, "supporting episode not found in space", map[string]any{"episode_id": episodeID, "space_id": spaceID}, nil)
		}
		supportingEpisodeIDs = append(supportingEpisodeIDs, episodeID)
	}
	if len(supportingEpisodeIDs) == 0 {
		return nil, errorf(ErrInputInvalid, "supporting_episode_ids must contain at least one episode", map[string]any{"field": "supporting_episode_ids"}, nil)
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
		SupportingEpisodeIDs: supportingEpisodeIDs,
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
	var result *SupersedeFactResult
	err := e.mutateLocked(func() error {
		var err error
		result, err = e.supersedeFactLocked(factID, input, reason)
		return err
	})
	if err != nil {
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
	var result *RetractFactResult
	err := e.mutateLocked(func() error {
		fact, ok := e.facts[factID]
		if !ok {
			return errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": factID}, nil)
		}
		if fact.Status == factStatusRetracted {
			result = &RetractFactResult{
				FactID: fact.ID,
				Status: fact.Status,
			}
			return nil
		}

		now := e.now()
		fact.Status = factStatusRetracted
		fact.RetractedAt = now
		fact.RetractionReason = reason
		fact.UpdatedAt = now
		e.facts[factID] = fact
		e.appendFactRevisionLocked(fact, "retract")
		result = &RetractFactResult{
			FactID: fact.ID,
			Status: fact.Status,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
		for _, legacyID := range []string{
			legacySourceID(spaceID, input.Kind, input.ExternalRef),
			normalizeLegacySourceID(input.Kind, input.ExternalRef),
		} {
			if source, ok := e.sources[legacyID]; ok && sourceMatches(source, spaceID, input.Kind, input.ExternalRef) {
				sourceID = legacyID
				break
			}
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
		Version:             currentStateVersion,
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

func (e *engine) saveLocked() error {
	if e.store == nil {
		return nil
	}
	return e.store.Save(e.snapshotLocked())
}

// mutateLocked runs fn under the engine lock and persists the mutated
// state. When fn or the save fails, in-memory state is rolled back to the
// pre-mutation snapshot so a failed write never leaks partial mutations.
func (e *engine) mutateLocked(fn func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return err
	}
	snapshot := e.snapshotLocked()
	if err := fn(); err != nil {
		e.restoreLocked(snapshot)
		return err
	}
	if err := e.saveLocked(); err != nil {
		e.restoreLocked(snapshot)
		return err
	}
	return nil
}

func (e *engine) restoreLocked(snapshot persistedState) {
	e.sequence = snapshot.Sequence
	e.sources = snapshot.Sources
	e.episodes = snapshot.Episodes
	e.entities = snapshot.Entities
	e.facts = snapshot.Facts
	e.factRevisions = snapshot.FactRevisions
	e.entityRevisions = snapshot.EntityRevisions
	e.migrationWatermarks = snapshot.MigrationWatermarks
}
