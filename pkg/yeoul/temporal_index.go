package yeoul

import "time"

// temporalIndex memoizes the newest revision of each record at one instant.
//
// An as-of read needs the latest revision at or before the requested time for
// every candidate record it visits. Deriving that per candidate rescans the
// whole global revision log, which makes a query cost about F x RF + E x RE.
// Resolving it once and looking the answer up per candidate makes the same
// query cost RF + RE plus one map lookup per candidate.
//
// The index is query-local scratch: a public read builds one while it holds the
// read lock, hands it to the helpers it calls, and drops it when the query
// returns. Nothing is cached on the engine, so no mutation has to invalidate it
// and no two readers share it.
type temporalIndex struct {
	engine *engine

	// Fact and entity histories are indexed independently. A read that needs
	// only one kind must not pay for the other kind's history: a fact lookup
	// would otherwise scan every entity revision in the database.
	factAt      time.Time
	factBuilt   bool
	entityAt    time.Time
	entityBuilt bool

	factRevisions   map[string]FactRevision
	entityRevisions map[string]EntityRevision
}

// temporalIndexRebuildObserver runs after the index scans the global revision
// maps. Tests set it to count how many scans a query performs, which is how the
// guard test proves one query builds the index once instead of rescanning the
// revision log for every candidate record. Production leaves it nil, so the
// observation costs a nil check.
var temporalIndexRebuildObserver func()

// temporalIndexKindObserver reports which revision kind a rebuild scanned.
// Tests set it to prove that a single-kind read never scans the other kind's
// revision log. Production leaves it nil.
var temporalIndexKindObserver func(kind string)

func newTemporalIndex(e *engine) *temporalIndex {
	return &temporalIndex{engine: e}
}

// rebuildFacts indexes the newest fact revision at or before at. A query that
// asks for a different instant after the index was built (a helper handed a
// different temporal filter, for example) rebuilds instead of answering from
// the wrong instant.
func (idx *temporalIndex) rebuildFacts(at time.Time) {
	idx.factAt = at
	idx.factBuilt = true

	facts := make(map[string]FactRevision)
	for _, revision := range idx.engine.factRevisions {
		if revision.TxTime.After(at) {
			continue
		}
		latest, ok := facts[revision.FactID]
		if !ok || revisionNewerThan(revision.TxTime, revision.ID, latest.TxTime, latest.ID) {
			facts[revision.FactID] = revision
		}
	}
	idx.factRevisions = facts

	idx.observeRebuild("fact")
}

// rebuildEntities is the entity half of rebuildFacts, with the same rebuild
// rule and the same reason for existing.
func (idx *temporalIndex) rebuildEntities(at time.Time) {
	idx.entityAt = at
	idx.entityBuilt = true

	entities := make(map[string]EntityRevision)
	for _, revision := range idx.engine.entityRevisions {
		if revision.TxTime.After(at) {
			continue
		}
		latest, ok := entities[revision.EntityID]
		if !ok || revisionNewerThan(revision.TxTime, revision.ID, latest.TxTime, latest.ID) {
			entities[revision.EntityID] = revision
		}
	}
	idx.entityRevisions = entities

	idx.observeRebuild("entity")
}

func (idx *temporalIndex) observeRebuild(kind string) {
	if temporalIndexRebuildObserver != nil {
		temporalIndexRebuildObserver()
	}
	if temporalIndexKindObserver != nil {
		temporalIndexKindObserver(kind)
	}
}

// revisionNewerThan applies the revision ordering used by the engine: the later
// transaction time wins, and equal transaction times break toward the larger
// revision ID so the result does not depend on map iteration order.
func revisionNewerThan(candidateTime time.Time, candidateID string, currentTime time.Time, currentID string) bool {
	if candidateTime.Equal(currentTime) {
		return candidateID > currentID
	}
	return candidateTime.After(currentTime)
}

func (idx *temporalIndex) latestFactRevisionAt(factID string, at time.Time) (FactRevision, bool) {
	if !idx.factBuilt || !idx.factAt.Equal(at) {
		idx.rebuildFacts(at)
	}
	revision, ok := idx.factRevisions[factID]
	return revision, ok
}

func (idx *temporalIndex) latestEntityRevisionAt(entityID string, at time.Time) (EntityRevision, bool) {
	if !idx.entityBuilt || !idx.entityAt.Equal(at) {
		idx.rebuildEntities(at)
	}
	revision, ok := idx.entityRevisions[entityID]
	return revision, ok
}
