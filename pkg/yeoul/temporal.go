package yeoul

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

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

func (e *engine) factVersionAt(fact Fact, filter TemporalFilter, index *temporalIndex) *Fact {
	at := asOfTime(filter)
	if at == nil {
		if !e.factVisibleAt(fact, filter) {
			return nil
		}
		return cloneFact(fact)
	}
	revision, ok := index.latestFactRevisionAt(fact.ID, *at)
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

// revisionComesAfter reports whether the revision identified by (id, txTime)
// is ordered after the one identified by (otherID, otherTxTime). A later
// transaction time always wins; equal transaction times (supersession
// deliberately creates them) fall back to the engine's numeric revision order
// so an ID digit boundary cannot hand the win to the older representation. The
// raw ID is only a last-resort tie-break between revisions that carry no
// recovered order.
//
// This is the engine's single revision ordering. The temporal index rebuilds
// its per-record latest maps with it, so an as-of read and a timeline sort
// cannot disagree about which representation of a record is newer.
func revisionComesAfter(id string, txTime time.Time, otherID string, otherTxTime time.Time) bool {
	if !txTime.Equal(otherTxTime) {
		return txTime.After(otherTxTime)
	}
	if order, otherOrder := revisionOrder(id), revisionOrder(otherID); order != otherOrder {
		return order > otherOrder
	}
	return id > otherID
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

func (e *engine) entityVersionAt(entity Entity, filter TemporalFilter, index *temporalIndex) *Entity {
	at := asOfTime(filter)
	if at == nil {
		return cloneEntity(entity)
	}
	if revision, ok := index.latestEntityRevisionAt(entity.ID, *at); ok {
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

func (e *engine) appendFactRevisionLocked(fact Fact, kind string) {
	revision := newFactRevision(e.newIDLocked(factRevisionIDPrefix), fact, kind, chooseTime(fact.UpdatedAt, e.now()))
	e.factRevisions[revision.ID] = revision
}

func (e *engine) appendEntityRevisionLocked(entity Entity, kind string) {
	revision := newEntityRevision(e.newIDLocked(entityRevisionIDPrefix), entity, kind, chooseTime(entity.UpdatedAt, e.now()))
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

func (e *engine) sourceVisibleAt(source Source, filter TemporalFilter) bool {
	if filter.AsOf != nil && source.CreatedAt.After(*filter.AsOf) {
		return false
	}
	return true
}

func (e *engine) matchesScopeForFact(fact Fact, scope ScopeFilter, filter TemporalFilter, index *temporalIndex) bool {
	if len(scope.FactStatus) > 0 && !slices.Contains(scope.FactStatus, fact.Status) {
		return false
	}
	if len(scope.EntityTypes) > 0 {
		subject, subjectOK := e.entities[fact.SubjectID]
		object, objectOK := e.entities[fact.ObjectID]
		if subjectOK {
			subjectOK = false
			if version := e.entityVersionAt(subject, filter, index); version != nil {
				subject = *version
				subjectOK = true
			}
		}
		if objectOK {
			objectOK = false
			if version := e.entityVersionAt(object, filter, index); version != nil {
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
// returned by derived indexes such as rax.

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

func ptrTime(t time.Time) *time.Time {
	return &t
}

func chooseTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	return fallback
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
