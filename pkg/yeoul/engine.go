package yeoul

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
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
	// historyInferredKey marks a record whose historical value was
	// reconstructed by the legacy revision seed rather than recorded by an
	// ordinary write. It is engine-managed: callers read it to learn that the
	// historical answer is inferred and may be incomplete.
	historyInferredKey = "history_inferred"
)

// reservedFactMetadataKeys are engine-managed fact keys: an ordinary fact write
// must not be able to set them, because they carry lifecycle state rather than
// caller context.
var reservedFactMetadataKeys = map[string]bool{
	"superseded_by":    true,
	"supersedes":       true,
	"supersede_reason": true,
	"duplicate_of":     true,
	historyInferredKey: true,
	entityHistoryKey:   true,
}

// reservedEntityMetadataKeys are the engine-managed entity keys an ordinary
// entity write must not set. The list is deliberately shorter than the fact
// list: "duplicate_of" is caller-supplied entity context here, written by the
// entity merge and compaction commands, so it stays writable. The two keys
// below are provenance: _history is the engine's own revision log, and
// history_inferred claims a historical answer was reconstructed, so accepting
// either from a caller would let a write forge the engine's provenance.
var reservedEntityMetadataKeys = map[string]bool{
	historyInferredKey: true,
	entityHistoryKey:   true,
}

type engine struct {
	mu       sync.RWMutex
	now      func() time.Time
	txTime   time.Time
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
	// Persisted state written before global id uniqueness was enforced can
	// already contain the same id under two kinds. Reject it here, before any
	// of it becomes graph or lookup state, rather than loading a database whose
	// nodes silently overwrite each other.
	if err := validateGlobalRecordIDs(*state); err != nil {
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
	if err := rejectSecretFields(episodeSecretFields(input)); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Kind) == "" {
		return nil, errorf(ErrInputInvalid, "episode kind is required", map[string]any{"field": "kind"}, nil)
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, errorf(ErrInputInvalid, "episode content is required", map[string]any{"field": "content"}, nil)
	}

	spaceID := normalizeSpaceID(input.SpaceID)

	var result *EpisodeResult
	err := e.mutateLocked(ctx, func() error {
		now := e.txNow()
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
	for _, episode := range input.Episodes {
		if err := rejectSecretFields(episodeSecretFields(episode)); err != nil {
			return nil, err
		}
	}
	for _, entity := range input.Entities {
		if err := rejectSecretFields(entitySecretFields(entity)); err != nil {
			return nil, err
		}
	}
	for _, fact := range input.Facts {
		fields := append(factSecretFields(fact), textSecretFields("fact.supporting_episode_ids", fact.SupportingEpisodeIDs)...)
		if err := rejectSecretFields(fields); err != nil {
			return nil, err
		}
	}
	result := &BatchResult{}
	err := e.mutateLocked(ctx, func() error {
		for _, episode := range input.Episodes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if strings.TrimSpace(episode.Kind) == "" || strings.TrimSpace(episode.Content) == "" {
				return errorf(ErrInputInvalid, "episode kind and content are required", map[string]any{"kind": episode.Kind, "id": episode.ID}, nil)
			}
			now := e.txNow()
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
			if err := ctx.Err(); err != nil {
				return err
			}
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
			if err := ctx.Err(); err != nil {
				return err
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
	if err := rejectSecretFields(entitySecretFields(input)); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Type) == "" {
		return nil, errorf(ErrInputInvalid, "entity type is required", map[string]any{"field": "type"}, nil)
	}
	if strings.TrimSpace(input.CanonicalName) == "" {
		return nil, errorf(ErrInputInvalid, "entity canonical_name is required", map[string]any{"field": "canonical_name"}, nil)
	}

	var entity *Entity
	err := e.mutateLocked(ctx, func() error {
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
	if err := e.ensureGlobalIDAvailableLocked(source.ID, kindSource); err != nil {
		return nil, err
	}
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
	} else if err := e.ensureGlobalIDAvailableLocked(episodeID, kindEpisode); err != nil {
		return nil, err
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
	// An entity revision carries no status field, so its metadata is the only
	// place a lifecycle marker can live. Ordinary entity writes therefore fail
	// closed on engine-managed keys exactly like assertFactLocked does, before
	// any state changes.
	for key := range input.Metadata {
		if reservedEntityMetadataKeys[key] {
			return nil, errorf(ErrLifecycleInvalid, "entity metadata key is lifecycle-managed", map[string]any{"metadata_key": key}, nil)
		}
	}
	now := e.txNow()
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
	// Both explicit and derived IDs are cross-checked. Derived IDs are
	// content-addressed, but an explicit episode, fact, or source ID can still
	// claim the same raw string first, so the global uniqueness invariant must
	// not depend on write order. Same-kind replays stay with the caller.
	if err := e.ensureGlobalIDAvailableLocked(id, kindEntity); err != nil {
		return nil, err
	}
	metadata := cloneAnyMap(input.Metadata)
	if metadata != nil {
		// stable_key is engine-managed: ordinary metadata must not be able to
		// change a stored strong identity.
		delete(metadata, "stable_key")
		// Engine-managed lifecycle keys must not be able to change stored
		// history semantics either. The ordinary write path already rejects
		// them above; stripping here keeps a future lifecycle-permitting path
		// from letting them ride into a seeded or recorded revision.
		for key := range reservedEntityMetadataKeys {
			delete(metadata, key)
		}
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
	if err := rejectSecretFields(append(factSecretFields(input), textSecretFields("fact.supporting_episode_ids", input.SupportingEpisodeIDs)...)); err != nil {
		return nil, err
	}
	if err := validateFactInput(input); err != nil {
		return nil, err
	}

	spaceID := normalizeSpaceID(input.SpaceID)
	var fact *Fact
	err := e.mutateLocked(ctx, func() error {
		var err error
		fact, err = e.assertFactLocked(spaceID, input, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return fact, nil
}

// validateFactInput enforces the normalized required inputs shared by every
// assertion path. It lives here, not in the public wrappers, so callers that
// reach assertFactLocked directly (IngestBatch and SupersedeFact) cannot skip
// provenance validation. Support is checked in its normalized form, so a list
// of only blank IDs is treated as empty.
func validateFactInput(input FactInput) error {
	if strings.TrimSpace(input.Predicate) == "" {
		return errorf(ErrInputInvalid, "fact predicate is required", map[string]any{"field": "predicate"}, nil)
	}
	if strings.TrimSpace(input.SubjectID) == "" {
		return errorf(ErrInputInvalid, "fact subject_id is required", map[string]any{"field": "subject_id"}, nil)
	}
	if len(dedupeStrings(input.SupportingEpisodeIDs)) == 0 {
		return errorf(ErrInputInvalid, "supporting_episode_ids must contain at least one episode", map[string]any{"field": "supporting_episode_ids"}, nil)
	}
	return nil
}

func (e *engine) assertFactLocked(spaceID string, input FactInput, allowLifecycleFields bool) (*Fact, error) {
	if err := validateFactInput(input); err != nil {
		return nil, err
	}
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
		return nil, errorf(ErrInputInvalid, "invalid fact validity interval", map[string]any{"fact_id": input.ID}, nil)
	}
	// A single-value slot is a guard, not a retirement: an assertion refuses an
	// occupied overlapping slot instead of silently retiring whatever happens to
	// hold it. The check runs before any ID is allocated or any revision is
	// appended, so a rejected conflict leaves every counter untouched.
	if !allowLifecycleFields && cardinality == factCardinalityOne {
		occupants := e.factSlotOccupantsLocked(spaceID, input.SubjectID, input.Predicate, input.ValidFrom, input.ValidTo)
		if len(occupants) > 0 {
			return nil, errorf(ErrFactConflict, "single-value fact slot is already occupied", map[string]any{
				"space_id":             spaceID,
				"subject_id":           input.SubjectID,
				"predicate":            input.Predicate,
				"conflicting_fact_ids": occupants,
			}, nil)
		}
	}

	now := e.txNow()
	id := input.ID
	if id == "" {
		id = e.newIDLocked("fact")
	} else if err := e.ensureGlobalIDAvailableLocked(id, kindFact); err != nil {
		return nil, err
	}
	if _, ok := e.facts[id]; ok {
		return nil, errorf(ErrLifecycleInvalid, "fact id already exists", map[string]any{"fact_id": id}, nil)
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
	e.facts[id] = fact
	e.appendFactRevisionLocked(fact, "assert")
	return cloneFact(fact), nil
}

// factSlotOccupantsLocked returns the sorted IDs of the active facts that hold
// the same space, subject, and predicate as the candidate and whose validity
// interval overlaps the candidate's. The result is sorted so a conflict report
// never depends on the order the fact map happened to be scanned. Interval
// overlap uses factValidityOverlaps, the same semantics a slot comparison has
// always used.
func (e *engine) factSlotOccupantsLocked(spaceID, subjectID, predicate string, validFrom, validTo time.Time) []string {
	occupants := make([]string, 0, 1)
	for id, fact := range e.facts {
		if fact.Status != factStatusActive || fact.SpaceID != spaceID || fact.SubjectID != subjectID || fact.Predicate != predicate {
			continue
		}
		if !factValidityOverlaps(fact.ValidFrom, fact.ValidTo, validFrom, validTo) {
			continue
		}
		occupants = append(occupants, id)
	}
	slices.Sort(occupants)
	return occupants
}

func (e *engine) SupersedeFact(ctx context.Context, factID string, input FactInput, reason string) (*SupersedeFactResult, error) {
	fields := append(factSecretFields(input), textSecretFields("fact.supporting_episode_ids", input.SupportingEpisodeIDs)...)
	fields = append(fields, reasonSecretField("supersede.reason", reason)...)
	if err := rejectSecretFields(fields); err != nil {
		return nil, err
	}
	var result *SupersedeFactResult
	err := e.mutateLocked(ctx, func() error {
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
	// The successor is created at the mutation's transaction time, and that same
	// instant becomes the retirement stamp of the old fact. It must land strictly
	// after the instant the old fact's current state became current: otherwise a
	// cut taken at that earlier stamp includes the retirement and loses the old
	// fact's active state whenever the clock cannot separate the two transitions.
	e.txTime = e.strictlyAfterTxTime(oldFact.UpdatedAt)
	// Supersession is strictly target-only, so the replacement must not trip the
	// single-value slot guard over the other occupants of the same slot.
	// Normalizing the internal assertion to the additive cardinality keeps the
	// guard honest for every public assertion path while this replacement stays
	// additive; the named target is the only fact retired below.
	if strings.TrimSpace(input.Cardinality) == factCardinalityOne {
		input.Cardinality = factCardinalityMany
	}
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
		// The replacement's lineage is exactly the fact it retires. The list form
		// is kept so snapshot and provenance consumers see one representation.
		storedNewFact.Metadata = mergeAnyMap(storedNewFact.Metadata, map[string]any{
			"supersedes":       []string{factID},
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
	if err := rejectSecretFields(reasonSecretField("retract.reason", reason)); err != nil {
		return nil, err
	}
	var result *RetractFactResult
	err := e.mutateLocked(ctx, func() error {
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

		// Ordered transitions of one fact must not share an instant. The
		// historical cut is expressed as a time, so a supersession and the
		// retraction that followed it are indistinguishable when both are stamped
		// with the same instant: a prefix taken at the supersession would still
		// expose the retraction. A POSIX wall clock resolves finely enough that
		// successive commits read distinct times, but Windows can report one
		// instant for both, so the stamp is carried past the transition it follows.
		now := e.txNow()
		if !now.After(fact.UpdatedAt) {
			now = fact.UpdatedAt.Add(time.Nanosecond)
		}
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
		record := e.entityVersionAt(entity, req.Temporal, newTemporalIndex(e))
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
			version = e.factVersionAt(*record, req.Temporal, newTemporalIndex(e))
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

// ResolveEntity looks an entity up by its identity tuple instead of by ID.
// The derived ID is a hash of the exact tuple, so a caller that only knows the
// identity cannot recompute the canonical ID when the stored namespace or type
// drifted (issue #139). Matching tolerates namespace/type case drift, an
// empty-vs-populated namespace, and a canonical name that matches a stored
// alias; every tolerated match is reported in Drifted so a caller can decide
// whether to reconcile it instead of silently reusing it.
func (e *engine) ResolveEntity(ctx context.Context, req EntityResolveRequest) (*EntityResolveResponse, error) {
	_ = ctx
	if strings.TrimSpace(req.Type) == "" {
		return nil, errorf(ErrInputInvalid, "entity type is required", map[string]any{"field": "type"}, nil)
	}
	if strings.TrimSpace(req.CanonicalName) == "" && strings.TrimSpace(req.StableKey) == "" {
		return nil, errorf(ErrInputInvalid, "canonical_name or stable_key is required", map[string]any{"field": "canonical_name"}, nil)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	spaceID := normalizeSpaceID(req.SpaceID)
	fold := func(value string) string {
		return strings.ToLower(strings.TrimSpace(value))
	}

	type resolution struct {
		entity Entity
		drift  bool
	}
	resolutions := make([]resolution, 0)
	for _, entity := range e.entities {
		if entity.SpaceID != spaceID || entityMarkedDuplicate(entity) {
			continue
		}

		// Type: an exact match is a clean match; a folded-only match is drift.
		drift := false
		if entity.Type != req.Type {
			if fold(entity.Type) != fold(req.Type) {
				continue
			}
			drift = true
		}

		// Namespace: two populated namespaces must agree after folding, and a
		// blank on exactly one side is tolerated drift.
		storedNamespaceBlank := strings.TrimSpace(entity.Namespace) == ""
		requestedNamespaceBlank := strings.TrimSpace(req.Namespace) == ""
		switch {
		case storedNamespaceBlank && requestedNamespaceBlank:
		case storedNamespaceBlank || requestedNamespaceBlank:
			drift = true
		case entity.Namespace != req.Namespace:
			if fold(entity.Namespace) != fold(req.Namespace) {
				continue
			}
			drift = true
		}

		// Identity: a strong key on both sides is compared exactly and the
		// display name is not consulted. When exactly one side carries a key,
		// only the near-duplicate guard (IncludeKeyDrift) widens the rule, and
		// then only on overlapping display names. With no key on either side
		// the display name decides, and a folded match (canonical name or
		// alias) is reported as drift so a caller sees the ambiguity instead
		// of silently reusing or duplicating the entity.
		storedKey := metadataStableKey(entity.Metadata)
		requestKeyed := strings.TrimSpace(req.StableKey) != ""
		storedKeyed := strings.TrimSpace(storedKey) != ""
		switch {
		case requestKeyed && storedKeyed:
			if req.StableKey != storedKey {
				continue
			}
		case requestKeyed != storedKeyed:
			if !req.IncludeKeyDrift || !entityDisplayNameMatches(entity, req.CanonicalName, fold) {
				continue
			}
			drift = true
		default:
			switch {
			case entity.CanonicalName == req.CanonicalName:
			case fold(entity.CanonicalName) == fold(req.CanonicalName):
				drift = true
			case slices.Contains(entity.Aliases, req.CanonicalName):
				drift = true
			case entityDisplayNameMatches(entity, req.CanonicalName, fold):
				drift = true
			default:
				continue
			}
		}

		resolutions = append(resolutions, resolution{entity: entity, drift: drift})
	}

	if len(resolutions) == 0 {
		return nil, errorf(ErrEntityNotFound, "no entity matches the requested identity", map[string]any{
			"namespace":      req.Namespace,
			"type":           req.Type,
			"canonical_name": req.CanonicalName,
			"stable_key":     req.StableKey,
			"space_id":       spaceID,
		}, nil)
	}

	sort.Slice(resolutions, func(i, j int) bool { return resolutions[i].entity.ID < resolutions[j].entity.ID })

	response := &EntityResolveResponse{
		Matches: make([]Entity, 0, len(resolutions)),
		Drifted: make([]Entity, 0, len(resolutions)),
	}
	for _, item := range resolutions {
		response.Matches = append(response.Matches, *cloneEntity(item.entity))
		if item.drift {
			response.Drifted = append(response.Drifted, *cloneEntity(item.entity))
		}
	}
	return response, nil
}

// entityDisplayNameMatches reports whether a request display name overlaps the
// entity's display identity after folding: the folded canonical name, or any
// folded alias. It is the widened overlap rule the near-duplicate guard uses
// when exactly one side carries a stable key, and it also catches a folded
// alias for the ordinary no-key rule.
func entityDisplayNameMatches(entity Entity, canonicalName string, fold func(string) string) bool {
	folded := fold(canonicalName)
	if fold(entity.CanonicalName) == folded {
		return true
	}
	for _, alias := range entity.Aliases {
		if fold(alias) == folded {
			return true
		}
	}
	return false
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

// mutatePreSaveHook runs after the mutation body returns and before the save
// starts. Tests replace it to cancel a request inside that window; production
// leaves it nil.
var mutatePreSaveHook func()

// mutateLocked runs fn under the engine lock and persists the mutated
// state. When fn or the save fails, in-memory state is rolled back to the
// pre-mutation snapshot so a failed write never leaks partial mutations.
func (e *engine) mutateLocked(ctx context.Context, fn func() error) error {
	// The caller's context governs the mutation. A canceled request is refused
	// before any work, and refused again once the write lock is acquired, because
	// a request can be abandoned while it waits for a concurrent writer.
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ensureWritableLocked(); err != nil {
		return err
	}
	// Pin one transaction time for the whole mutation so every record it creates
	// shares a single commit instant. An atomic batch then becomes visible all at
	// once: no historical cut can fall between the system times of its records.
	e.txTime = e.now()
	defer func() { e.txTime = time.Time{} }()
	snapshot := e.snapshotLocked()
	if err := fn(); err != nil {
		e.restoreLocked(snapshot)
		return err
	}
	if mutatePreSaveHook != nil {
		mutatePreSaveHook()
	}
	// Re-check after the mutation body and before the save starts. A request
	// can be abandoned in the window between the last in-memory change and the
	// commit, and that window is still pre-commit work: nothing durable exists
	// yet, so the mutation must roll back rather than commit an abandoned write.
	if err := ctx.Err(); err != nil {
		e.restoreLocked(snapshot)
		return err
	}
	if err := e.saveLocked(); err != nil {
		e.restoreLocked(snapshot)
		return err
	}
	// The save is the commit point. The context is not consulted once the save
	// has begun, so cancellation racing the commit cannot mislabel durable work
	// as skipped; the check above covers only the pre-commit window.
	return nil
}

// txNow returns the system time for records created by the current mutation.
// mutateLocked pins one transaction time per mutation, so an atomic batch is
// stamped as one commit. Outside a mutation there is no pinned time and callers
// get the wall clock, which keeps ReadOnly queries and metadata timestamps
// unchanged.
func (e *engine) txNow() time.Time {
	if !e.txTime.IsZero() {
		return e.txTime
	}
	return e.now()
}

// strictlyAfterTxTime returns the mutation's pinned transaction time carried
// strictly past `after`. Ordered transitions of one fact must not share an
// instant: the historical cut is expressed as a time, so a retirement and the
// successor it creates are indistinguishable when both are stamped with the
// same instant as the state they replace. A POSIX wall clock resolves finely
// enough that successive commits read distinct times, but Windows can report
// one instant for both, so the successor's creation is carried past the state
// it retires and a cut at that earlier stamp stays a valid pre-transition cut.
func (e *engine) strictlyAfterTxTime(after time.Time) time.Time {
	now := e.txNow()
	if !now.After(after) {
		now = after.Add(time.Nanosecond)
	}
	return now
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
