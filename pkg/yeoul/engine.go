package yeoul

import (
	"context"
	"fmt"
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
	entityHistoryKey     = "_history"
)

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
		cfg:      cfg,
		store:    store,
		sources:  make(map[string]Source),
		episodes: make(map[string]Episode),
		entities: make(map[string]Entity),
		facts:    make(map[string]Fact),
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
	source, err := e.resolveSource(input.SourceID, input.Source, now)
	if err != nil {
		return nil, err
	}
	source.SpaceID = normalizeSpaceID(firstNonEmpty(source.SpaceID, input.SpaceID))

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
	result, err := e.ingestEpisodeLocked(spaceID, input, source, now)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
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
	}

	for _, episode := range input.Episodes {
		if strings.TrimSpace(episode.Kind) == "" || strings.TrimSpace(episode.Content) == "" {
			restore()
			return nil, errorf(ErrInputInvalid, "episode kind and content are required", map[string]any{"kind": episode.Kind, "id": episode.ID}, nil)
		}
		now := e.now()
		source, err := e.resolveSource(episode.SourceID, episode.Source, now)
		if err != nil {
			restore()
			return nil, err
		}
		source.SpaceID = normalizeSpaceID(firstNonEmpty(source.SpaceID, episode.SpaceID))
		item, err := e.ingestEpisodeLocked(normalizeSpaceID(episode.SpaceID), episode, source, now)
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
		item, err := e.assertFactLocked(normalizeSpaceID(fact.SpaceID), fact)
		if err != nil {
			restore()
			return nil, err
		}
		result.FactIDs = append(result.FactIDs, item.ID)
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
	entity, err := e.upsertEntityLocked(input)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		return nil, err
	}
	return entity, nil
}

func (e *engine) ingestEpisodeLocked(spaceID string, input EpisodeInput, source Source, now time.Time) (*EpisodeResult, error) {
	if _, ok := e.sources[source.ID]; !ok {
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
		id = normalizeEntityID(input.Namespace, input.Type, input.CanonicalName)
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
			Metadata:      cloneAnyMap(input.Metadata),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		e.entities[id] = entity
		return cloneEntity(entity), nil
	}

	if entity.SpaceID != "" && entity.SpaceID != spaceID {
		return nil, errorf(ErrLifecycleInvalid, "entity id already exists in another space", map[string]any{
			"entity_id": id,
		}, nil)
	}
	entity.Metadata = appendEntityHistory(entity.Metadata, entitySnapshotEntry(entity, now))
	entity.SpaceID = firstNonEmpty(entity.SpaceID, spaceID)
	entity.Namespace = firstNonEmpty(entity.Namespace, input.Namespace)
	entity.Type = input.Type
	entity.CanonicalName = input.CanonicalName
	entity.Aliases = dedupeStrings(append(entity.Aliases, input.Aliases...))
	entity.Metadata = mergeAnyMap(entity.Metadata, input.Metadata)
	entity.UpdatedAt = now
	e.entities[id] = entity
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
	fact, err := e.assertFactLocked(spaceID, input)
	if err != nil {
		return nil, err
	}
	if err := e.saveLocked(); err != nil {
		return nil, err
	}
	return fact, nil
}

func (e *engine) assertFactLocked(spaceID string, input FactInput) (*Fact, error) {
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
	e.facts[id] = fact
	return cloneFact(fact), nil
}

func (e *engine) SupersedeFact(ctx context.Context, factID string, input FactInput, reason string) (*SupersedeFactResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureWritableLocked(); err != nil {
		return nil, err
	}
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
	spaceID := firstNonEmpty(input.SpaceID, oldFact.SpaceID)
	newFact, err := e.assertFactLocked(spaceID, input)
	if err != nil {
		return nil, err
	}
	oldFact.Status = factStatusSuperseded
	if !input.ValidFrom.IsZero() {
		oldFact.ValidTo = input.ValidFrom
	}
	oldFact.UpdatedAt = e.now()
	oldFact.Metadata = mergeAnyMap(oldFact.Metadata, map[string]any{
		"superseded_by":    newFact.ID,
		"supersede_reason": reason,
	})
	e.facts[factID] = oldFact
	if storedNewFact, ok := e.facts[newFact.ID]; ok {
		storedNewFact.Metadata = mergeAnyMap(storedNewFact.Metadata, map[string]any{
			"supersedes":       factID,
			"supersede_reason": reason,
		})
		e.facts[newFact.ID] = storedNewFact
		newFact = cloneFact(storedNewFact)
	}
	if err := e.saveLocked(); err != nil {
		return nil, err
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
	if err := e.saveLocked(); err != nil {
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
		record, err := e.GetEntity(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		record = e.entityVersionAt(*record, req.Temporal)
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
		if record.SpaceID != meta.SpaceID {
			return nil, errorf(ErrQueryFailed, "record not found in space", map[string]any{"id": req.ID, "space_id": meta.SpaceID}, nil)
		}
		if req.Temporal.AsOf != nil && !e.factVisibleAt(*record, req.Temporal) {
			return nil, errorf(ErrQueryFailed, "record not visible at requested time", map[string]any{"id": req.ID}, nil)
		}
		return &GetRecordResponse{Meta: meta, Kind: req.Kind, Record: record}, nil
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

	if slices.Contains(types, "fact") {
		for _, fact := range e.facts {
			if fact.SpaceID != spaceID || !e.factVisibleAt(fact, req.Temporal) || !matchesScopeForFact(fact, req.Scope, e.episodes) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, fact.Predicate) {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, append([]string{fact.ID, fact.SubjectID, fact.ObjectID}, fact.SupportingEpisodeIDs...)...)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			text := strings.ToLower(fact.ValueText + " " + fact.Predicate + " " + fact.SubjectID + " " + fact.ObjectID)
			matched, baseScore, reason := matchSearch(mode, query, text)
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
					HitID:       "hit_" + fact.ID,
					HitType:     "fact",
					RecordID:    fact.ID,
					Score:       score,
					MatchedText: fact.ValueText,
					Reasons:     reasons,
				})
				included.Facts = append(included.Facts, fact)
				e.addFactSupport(&included, fact)
			}
		}
	}
	if slices.Contains(types, "episode") {
		for _, episode := range e.episodes {
			if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) || !matchesScopeForEpisode(episode, req.Scope) {
				continue
			}
			if len(req.Predicates) > 0 {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			matched, baseScore, reason := matchSearch(mode, query, strings.ToLower(episode.Content))
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
				included.Episodes = append(included.Episodes, episode)
				if source, ok := e.sources[episode.SourceID]; ok {
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
			matched, baseScore, reason := matchSearch(mode, query, text)
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
				included.Entities = append(included.Entities, *entityRecord)
			}
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
		if fact.SpaceID != spaceID || !e.factVisibleAt(fact, req.Temporal) || !matchesScopeForFact(fact, req.Scope, e.episodes) {
			continue
		}
		if len(req.SubjectIDs) > 0 && !slices.Contains(req.SubjectIDs, fact.SubjectID) {
			continue
		}
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, fact.Predicate) {
			continue
		}
		if len(req.ObjectIDs) > 0 && !slices.Contains(req.ObjectIDs, fact.ObjectID) {
			continue
		}
		if req.ObjectText != "" && !strings.Contains(strings.ToLower(fact.ValueText), strings.ToLower(req.ObjectText)) {
			continue
		}
		facts = append(facts, fact)
		included.Facts = append(included.Facts, fact)
		e.addFactSupport(&included, fact)
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
		if entity.SpaceID != spaceID || !e.entityVisibleAt(entity, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: entity.ID, Type: "Entity", Label: entity.CanonicalName})
	}
	for _, source := range e.sources {
		if source.SpaceID != spaceID || !e.sourceVisibleAt(source, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: source.ID, Type: "Source", Label: source.Kind})
	}
	for _, episode := range e.episodes {
		if episode.SpaceID != spaceID || !matchesScopeForEpisode(episode, req.Scope) || !e.episodeVisibleAt(episode, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: episode.ID, Type: "Episode", Label: episode.Kind})
		if episode.SourceID != "" {
			addEdge(GraphEdge{ID: "edge:" + episode.ID + ":source", Type: "FROM_SOURCE", FromID: episode.ID, ToID: episode.SourceID})
		}
	}
	for _, fact := range e.facts {
		if fact.SpaceID != spaceID || !matchesScopeForFact(fact, req.Scope, e.episodes) || !e.factVisibleAt(fact, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: fact.ID, Type: "Fact", Label: fact.Predicate})
		addEdge(GraphEdge{ID: "edge:" + fact.ID + ":subject", Type: "SUBJECT", FromID: fact.ID, ToID: fact.SubjectID})
		if fact.ObjectID != "" {
			addEdge(GraphEdge{ID: "edge:" + fact.ID + ":object", Type: "OBJECT", FromID: fact.ID, ToID: fact.ObjectID})
		}
		for _, episodeID := range fact.SupportingEpisodeIDs {
			if _, ok := e.episodes[episodeID]; ok {
				addEdge(GraphEdge{ID: "edge:" + fact.ID + ":" + episodeID, Type: "ASSERTS", FromID: episodeID, ToID: fact.ID})
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
		if episode.SpaceID != spaceID || !matchesScopeForEpisode(episode, req.Scope) || !matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID) {
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
		if !timelineEventVisible(event, req.Temporal) {
			continue
		}
		addIfAllowed(event)
	}
	for _, fact := range e.facts {
		if fact.SpaceID != spaceID || !matchesScopeForFact(fact, req.Scope, e.episodes) || !matchesAnchors(req.AnchorIDs, append([]string{fact.SubjectID, fact.ObjectID, fact.ID}, fact.SupportingEpisodeIDs...)...) {
			continue
		}
		createdEvent := TimelineEvent{
			EventID:    "evt:" + fact.ID + ":created",
			EventType:  "fact_created",
			RecordType: "fact",
			RecordID:   fact.ID,
			Timestamp:  chooseTime(fact.ObservedAt, fact.CreatedAt),
			Summary:    fact.Predicate,
		}
		if timelineEventVisible(createdEvent, req.Temporal) {
			addIfAllowed(createdEvent)
		}
		if fact.Status == factStatusSuperseded {
			event := TimelineEvent{
				EventID:    "evt:" + fact.ID + ":superseded",
				EventType:  "fact_superseded",
				RecordType: "fact",
				RecordID:   fact.ID,
				Timestamp:  chooseTime(fact.ValidTo, fact.UpdatedAt),
				Summary:    fact.Predicate,
			}
			if timelineEventVisible(event, req.Temporal) {
				addIfAllowed(event)
			}
		}
		if fact.Status == factStatusRetracted {
			event := TimelineEvent{
				EventID:    "evt:" + fact.ID + ":retracted",
				EventType:  "fact_retracted",
				RecordType: "fact",
				RecordID:   fact.ID,
				Timestamp:  chooseTime(fact.RetractedAt, fact.UpdatedAt),
				Summary:    fact.RetractionReason,
			}
			if timelineEventVisible(event, req.Temporal) {
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
		if !ok || fact.SpaceID != spaceID || !e.factVisibleAt(fact, req.Temporal) {
			return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": req.ID}, nil)
		}
		root.Label = fact.Predicate
		root.Meta = factProvenanceMeta(fact)
		nodes = append(nodes, root)
		for _, episodeID := range fact.SupportingEpisodeIDs {
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
		if nextID, _ := fact.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 1 {
			if nextFact, ok := e.facts[nextID]; ok && nextFact.SpaceID == spaceID {
				if addNode(ProvenanceNode{ID: nextFact.ID, Type: "Fact", Label: nextFact.Predicate, Meta: factProvenanceMeta(nextFact)}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + nextFact.ID + ":" + fact.ID + ":supersedes", Type: "SUPERSEDES", FromID: nextFact.ID, ToID: fact.ID}, 1)
				}
			}
		}
		if previousID, _ := fact.Metadata["supersedes"].(string); previousID != "" && maxDepth >= 1 {
			if previousFact, ok := e.facts[previousID]; ok && previousFact.SpaceID == spaceID {
				if addNode(ProvenanceNode{ID: previousFact.ID, Type: "Fact", Label: previousFact.Predicate, Meta: factProvenanceMeta(previousFact)}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + fact.ID + ":" + previousFact.ID + ":supersedes", Type: "SUPERSEDES", FromID: fact.ID, ToID: previousFact.ID}, 1)
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
			if fact.SpaceID != spaceID || !e.factVisibleAt(fact, req.Temporal) || (fact.SubjectID != entityRecord.ID && fact.ObjectID != entityRecord.ID) {
				continue
			}
			if !addNode(ProvenanceNode{ID: fact.ID, Type: "Fact", Label: fact.Predicate}, 1) {
				continue
			}
			edgeType := "SUBJECT"
			if fact.ObjectID == entityRecord.ID {
				edgeType = "OBJECT"
			}
			addEdge(ProvenanceEdge{ID: "prov:" + fact.ID + ":" + entityRecord.ID, Type: edgeType, FromID: fact.ID, ToID: entityRecord.ID}, 1)
			if maxDepth < 2 {
				continue
			}
			for _, episodeID := range fact.SupportingEpisodeIDs {
				episode, ok := e.episodes[episodeID]
				if !ok || !e.episodeVisibleAt(episode, req.Temporal) {
					continue
				}
				if addNode(ProvenanceNode{ID: episode.ID, Type: "Episode", Label: episode.Kind}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + fact.ID, Type: "ASSERTS", FromID: episode.ID, ToID: fact.ID}, 2)
				}
			}
			if nextID, _ := fact.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 2 {
				if nextFact, ok := e.facts[nextID]; ok && nextFact.SpaceID == spaceID && addNode(ProvenanceNode{ID: nextFact.ID, Type: "Fact", Label: nextFact.Predicate, Meta: factProvenanceMeta(nextFact)}, 2) {
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
				if fact.SpaceID != spaceID || !e.factVisibleAt(fact, req.Temporal) || !slices.Contains(fact.SupportingEpisodeIDs, episode.ID) {
					continue
				}
				if addNode(ProvenanceNode{ID: fact.ID, Type: "Fact", Label: fact.Predicate}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + fact.ID, Type: "ASSERTS", FromID: episode.ID, ToID: fact.ID}, 1)
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

func (e *engine) resolveSource(sourceID string, input SourceInput, now time.Time) (Source, error) {
	if sourceID == "" && input.ID != "" {
		sourceID = input.ID
	}
	if sourceID == "" {
		if strings.TrimSpace(input.Kind) == "" {
			input.Kind = "inline"
		}
		sourceID = normalizeSourceID(input.Kind, input.ExternalRef)
	}
	if sourceID == "" {
		sourceID = "src-" + fmt.Sprintf("%d", now.UnixNano())
	}
	return Source{
		ID:          sourceID,
		Kind:        input.Kind,
		URI:         input.URI,
		ExternalRef: input.ExternalRef,
		Metadata:    cloneAnyMap(input.Metadata),
		CreatedAt:   now,
	}, nil
}

func (e *engine) addFactSupport(included *IncludedRecords, fact Fact) {
	if entity, ok := e.entities[fact.SubjectID]; ok {
		included.Entities = append(included.Entities, entity)
	}
	if fact.ObjectID != "" {
		if entity, ok := e.entities[fact.ObjectID]; ok {
			included.Entities = append(included.Entities, entity)
		}
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		if episode, ok := e.episodes[episodeID]; ok {
			included.Episodes = append(included.Episodes, episode)
			if source, ok := e.sources[episode.SourceID]; ok {
				included.Sources = append(included.Sources, source)
			}
		}
	}
}

func (e *engine) factVisibleAt(fact Fact, filter TemporalFilter) bool {
	if filter.AsOf != nil {
		at := *filter.AsOf
		start := chooseTime(fact.ValidFrom, chooseTime(fact.ObservedAt, fact.CreatedAt))
		if !start.IsZero() && start.After(at) {
			return false
		}
		end := fact.ValidTo
		if end.IsZero() && !fact.RetractedAt.IsZero() {
			end = fact.RetractedAt
		}
		if end.IsZero() && fact.Status == factStatusSuperseded {
			end = fact.UpdatedAt
		}
		if end.IsZero() && fact.Status == factStatusRetracted {
			end = chooseTime(fact.RetractedAt, fact.UpdatedAt)
		}
		if !end.IsZero() && !end.After(at) {
			return false
		}
	} else if !filter.IncludeInactive && fact.Status != factStatusActive {
		return false
	}
	observedAt := chooseTime(fact.ObservedAt, fact.CreatedAt)
	if filter.ObservedFrom != nil && observedAt.Before(*filter.ObservedFrom) {
		return false
	}
	if filter.ObservedTo != nil && observedAt.After(*filter.ObservedTo) {
		return false
	}
	return true
}

func (e *engine) episodeVisibleAt(episode Episode, filter TemporalFilter) bool {
	if filter.AsOf != nil {
		at := *filter.AsOf
		if chooseTime(episode.ObservedAt, episode.IngestedAt).After(at) {
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
	if filter.AsOf != nil && entity.CreatedAt.After(*filter.AsOf) {
		return false
	}
	return true
}

func (e *engine) entityVersionAt(entity Entity, filter TemporalFilter) *Entity {
	if !e.entityVisibleAt(entity, filter) {
		return nil
	}
	if filter.AsOf == nil || !entity.UpdatedAt.After(*filter.AsOf) {
		return cloneEntity(entity)
	}

	snapshots := entityHistorySnapshots(entity.Metadata)
	if len(snapshots) == 0 {
		return cloneEntity(entity)
	}
	asOf := *filter.AsOf
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ChangedAt.Before(snapshots[j].ChangedAt) })
	for _, snapshot := range snapshots {
		if snapshot.ChangedAt.After(asOf) {
			return snapshot.toEntity(entity.ID)
		}
	}
	return cloneEntity(entity)
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
	if supersedes, _ := fact.Metadata["supersedes"].(string); supersedes != "" {
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

func matchesScopeForFact(fact Fact, scope ScopeFilter, episodes map[string]Episode) bool {
	if len(scope.FactStatus) > 0 && !slices.Contains(scope.FactStatus, fact.Status) {
		return false
	}
	if len(scope.GroupIDs) == 0 {
		return true
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		if episode, ok := episodes[episodeID]; ok && slices.Contains(scope.GroupIDs, episode.GroupID) {
			return true
		}
	}
	return false
}

func matchesScopeForEpisode(episode Episode, scope ScopeFilter) bool {
	if len(scope.GroupIDs) > 0 && !slices.Contains(scope.GroupIDs, episode.GroupID) {
		return false
	}
	if len(scope.SourceIDs) > 0 && !slices.Contains(scope.SourceIDs, episode.SourceID) {
		return false
	}
	return true
}

func matchesEntityType(entity Entity, types []string) bool {
	return len(types) == 0 || slices.Contains(types, entity.Type)
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

func timelineEventVisible(event TimelineEvent, filter TemporalFilter) bool {
	if filter.AsOf != nil && event.Timestamp.After(*filter.AsOf) {
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

	overlap := tokenOverlapScore(query, text)
	switch mode {
	case SearchModeKeyword:
		return false, 0, ""
	case SearchModeSemantic:
		if overlap <= 0 {
			return false, 0, ""
		}
		return true, 0.5 + overlap*0.5, "semantic_token_overlap"
	default:
		if overlap <= 0 {
			return false, 0, ""
		}
		return true, 0.35 + overlap*0.45, "hybrid_token_overlap"
	}
}

func tokenOverlapScore(query, text string) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	textTokens := make(map[string]struct{})
	for _, token := range tokenize(text) {
		textTokens[token] = struct{}{}
	}
	if len(textTokens) == 0 {
		return 0
	}
	matches := 0
	for _, token := range queryTokens {
		if _, ok := textTokens[token]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
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
	parts := []string{normalizeIDPart(namespace), normalizeIDPart(entityType), normalizeIDPart(canonical)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

func normalizeSourceID(kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
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
	next := atomic.AddUint64(&e.sequence, 1)
	return fmt.Sprintf("%s_%06d", prefix, next)
}

func (e *engine) applyState(state persistedState) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sequence = state.Sequence
	e.sources = defaultSources(state.Sources)
	e.episodes = defaultEpisodes(state.Episodes)
	e.entities = defaultEntities(state.Entities)
	e.facts = defaultFacts(state.Facts)
}

func (e *engine) snapshotLocked() persistedState {
	return clonePersistedState(persistedState{
		Version:  1,
		Sequence: e.sequence,
		Sources:  e.sources,
		Episodes: e.episodes,
		Entities: e.entities,
		Facts:    e.facts,
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
		dst[k] = v
	}
	return dst
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
