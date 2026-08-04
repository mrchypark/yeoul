package yeoul

import (
	"time"
)

// dirtyTracker는 변경된 항목만 추적하는 시스템입니다.
type dirtyTracker struct {
	sources         map[string]dirtyEntry[Source]
	episodes        map[string]dirtyEntry[Episode]
	entities        map[string]dirtyEntry[Entity]
	facts           map[string]dirtyEntry[Fact]
	factRevisions   map[string]dirtyEntry[FactRevision]
	entityRevisions map[string]dirtyEntry[EntityRevision]
	watermarks      map[string]dirtyEntry[MigrationWatermark]
	sequence        uint64
}

type dirtyEntry[T any] struct {
	current  T
	original T
	isDirty  bool
	isNew    bool
}

func newDirtyTracker() *dirtyTracker {
	return &dirtyTracker{
		sources:         make(map[string]dirtyEntry[Source]),
		episodes:        make(map[string]dirtyEntry[Episode]),
		entities:        make(map[string]dirtyEntry[Entity]),
		facts:           make(map[string]dirtyEntry[Fact]),
		factRevisions:   make(map[string]dirtyEntry[FactRevision]),
		entityRevisions: make(map[string]dirtyEntry[EntityRevision]),
		watermarks:      make(map[string]dirtyEntry[MigrationWatermark]),
	}
}

// initFromState는 기존 상태로 트래커를 초기화합니다.
func (dt *dirtyTracker) initFromState(state persistedState) {
	dt.sequence = state.Sequence

	for id, source := range state.Sources {
		dt.sources[id] = dirtyEntry[Source]{current: source, original: source, isDirty: false, isNew: false}
	}
	for id, episode := range state.Episodes {
		dt.episodes[id] = dirtyEntry[Episode]{current: episode, original: episode, isDirty: false, isNew: false}
	}
	for id, entity := range state.Entities {
		dt.entities[id] = dirtyEntry[Entity]{current: entity, original: entity, isDirty: false, isNew: false}
	}
	for id, fact := range state.Facts {
		dt.facts[id] = dirtyEntry[Fact]{current: fact, original: fact, isDirty: false, isNew: false}
	}
	for id, revision := range state.FactRevisions {
		dt.factRevisions[id] = dirtyEntry[FactRevision]{current: revision, original: revision, isDirty: false, isNew: false}
	}
	for id, revision := range state.EntityRevisions {
		dt.entityRevisions[id] = dirtyEntry[EntityRevision]{current: revision, original: revision, isDirty: false, isNew: false}
	}
	for id, watermark := range state.MigrationWatermarks {
		dt.watermarks[id] = dirtyEntry[MigrationWatermark]{current: watermark, original: watermark, isDirty: false, isNew: false}
	}
}

// markSource는 소스의 변경을 추적합니다.
func (dt *dirtyTracker) markSource(id string, source Source) {
	entry, exists := dt.sources[id]
	if !exists {
		dt.sources[id] = dirtyEntry[Source]{current: source, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(source) {
		entry.current = source
		entry.isDirty = true
		dt.sources[id] = entry
	}
}

// markEpisode는 에피소드의 변경을 추적합니다.
func (dt *dirtyTracker) markEpisode(id string, episode Episode) {
	entry, exists := dt.episodes[id]
	if !exists {
		dt.episodes[id] = dirtyEntry[Episode]{current: episode, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(episode) {
		entry.current = episode
		entry.isDirty = true
		dt.episodes[id] = entry
	}
}

// markEntity는 엔티티의 변경을 추적합니다.
func (dt *dirtyTracker) markEntity(id string, entity Entity) {
	entry, exists := dt.entities[id]
	if !exists {
		dt.entities[id] = dirtyEntry[Entity]{current: entity, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(entity) {
		entry.current = entity
		entry.isDirty = true
		dt.entities[id] = entry
	}
}

// markFact는 팩트의 변경을 추적합니다.
func (dt *dirtyTracker) markFact(id string, fact Fact) {
	entry, exists := dt.facts[id]
	if !exists {
		dt.facts[id] = dirtyEntry[Fact]{current: fact, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(fact) {
		entry.current = fact
		entry.isDirty = true
		dt.facts[id] = entry
	}
}

// markFactRevision는 팩트 리비전의 변경을 추적합니다.
func (dt *dirtyTracker) markFactRevision(id string, revision FactRevision) {
	entry, exists := dt.factRevisions[id]
	if !exists {
		dt.factRevisions[id] = dirtyEntry[FactRevision]{current: revision, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(revision) {
		entry.current = revision
		entry.isDirty = true
		dt.factRevisions[id] = entry
	}
}

// markEntityRevision는 엔티티 리비전의 변경을 추적합니다.
func (dt *dirtyTracker) markEntityRevision(id string, revision EntityRevision) {
	entry, exists := dt.entityRevisions[id]
	if !exists {
		dt.entityRevisions[id] = dirtyEntry[EntityRevision]{current: revision, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(revision) {
		entry.current = revision
		entry.isDirty = true
		dt.entityRevisions[id] = entry
	}
}

// markWatermark는 마이그레이션 워크마크의 변경을 추적합니다.
func (dt *dirtyTracker) markWatermark(id string, watermark MigrationWatermark) {
	entry, exists := dt.watermarks[id]
	if !exists {
		dt.watermarks[id] = dirtyEntry[MigrationWatermark]{current: watermark, isNew: true, isDirty: true}
		return
	}
	if !entry.current.Equal(watermark) {
		entry.current = watermark
		entry.isDirty = true
		dt.watermarks[id] = entry
	}
}

// markSourceDeleted는 소스의 삭제를 추적합니다.
func (dt *dirtyTracker) markSourceDeleted(id string) {
	if entry, exists := dt.sources[id]; exists {
		entry.isDirty = true
		entry.current = Source{} // 빈 구조체로 표시
		dt.sources[id] = entry
	}
}

// markEpisodeDeleted는 에피소드의 삭제를 추적합니다.
func (dt *dirtyTracker) markEpisodeDeleted(id string) {
	if entry, exists := dt.episodes[id]; exists {
		entry.isDirty = true
		entry.current = Episode{}
		dt.episodes[id] = entry
	}
}

// markEntityDeleted는 엔티티의 삭제를 추적합니다.
func (dt *dirtyTracker) markEntityDeleted(id string) {
	if entry, exists := dt.entities[id]; exists {
		entry.isDirty = true
		entry.current = Entity{}
		dt.entities[id] = entry
	}
}

// markFactDeleted는 팩트의 삭제를 추적합니다.
func (dt *dirtyTracker) markFactDeleted(id string) {
	if entry, exists := dt.facts[id]; exists {
		entry.isDirty = true
		entry.current = Fact{}
		dt.facts[id] = entry
	}
}

// getDirtyState는 변경된 항목만 포함된 상태를 반환합니다.
func (dt *dirtyTracker) getDirtyState(current persistedState) (persistedState, bool) {
	dirty := persistedState{
		Version:             current.Version,
		Sequence:            current.Sequence,
		Sources:             make(map[string]Source),
		Episodes:            make(map[string]Episode),
		Entities:            make(map[string]Entity),
		Facts:               make(map[string]Fact),
		FactRevisions:       make(map[string]FactRevision),
		EntityRevisions:     make(map[string]EntityRevision),
		MigrationWatermarks: make(map[string]MigrationWatermark),
	}

	hasChanges := false

	// 변경된 소스 수집
	for id, entry := range dt.sources {
		if entry.isDirty {
			if isEmptySource(entry.current) {
				// 삭제된 항목은 빈 맵에 포함하지 않음
			} else {
				dirty.Sources[id] = entry.current
			}
			hasChanges = true
		}
	}

	// 변경된 에피소드 수집
	for id, entry := range dt.episodes {
		if entry.isDirty {
			if isEmptyEpisode(entry.current) {
				// 삭제됨
			} else {
				dirty.Episodes[id] = entry.current
			}
			hasChanges = true
		}
	}

	// 변경된 엔티티 수집
	for id, entry := range dt.entities {
		if entry.isDirty {
			if isEmptyEntity(entry.current) {
				// 삭제됨
			} else {
				dirty.Entities[id] = entry.current
			}
			hasChanges = true
		}
	}

	// 변경된 팩트 수집
	for id, entry := range dt.facts {
		if entry.isDirty {
			if isEmptyFact(entry.current) {
				// 삭제됨
			} else {
				dirty.Facts[id] = entry.current
			}
			hasChanges = true
		}
	}

	// 변경된 팩트 리비전 수집
	for id, entry := range dt.factRevisions {
		if entry.isDirty {
			dirty.FactRevisions[id] = entry.current
			hasChanges = true
		}
	}

	// 변경된 엔티티 리비전 수집
	for id, entry := range dt.entityRevisions {
		if entry.isDirty {
			dirty.EntityRevisions[id] = entry.current
			hasChanges = true
		}
	}

	// 변경된 워크마크 수집
	for id, entry := range dt.watermarks {
		if entry.isDirty {
			dirty.MigrationWatermarks[id] = entry.current
			hasChanges = true
		}
	}

	return dirty, hasChanges
}

// reset는 트래커를 리셋합니다.
func (dt *dirtyTracker) reset(state persistedState) {
	dt.initFromState(state)
}

// isEmpty* 함수들은 구조체가 비어있는지 확인합니다.
func isEmptySource(s Source) bool {
	return s.ID == ""
}

func isEmptyEpisode(e Episode) bool {
	return e.ID == ""
}

func isEmptyEntity(e Entity) bool {
	return e.ID == ""
}

func isEmptyFact(f Fact) bool {
	return f.ID == ""
}

// deltaStatements는 변경된 항목만 포함된 Cypher 문장을 생성합니다.
func (dt *dirtyTracker) buildOptimizedDeltaStatements(prev, next persistedState) []string {
	statements := make([]string, 0, 64)

	// 메타 변경 확인
	if prev.Sequence != next.Sequence {
		statements = append(statements,
			"MATCH (m:YeoulMeta {id:'singleton'}) DELETE m",
			"CREATE (:YeoulMeta {id:'singleton', sequence:"+cypherUint64Literal(next.Sequence)+"})",
		)
	}

	// 변경된 소스 처리
	for id := range dt.sources {
		entry := dt.sources[id]
		if !entry.isDirty {
			continue
		}
		oldSource, oldExists := prev.Sources[id]
		newSource, newExists := next.Sources[id]

		switch {
		case !oldExists && newExists:
			statements = append(statements, createSourceStatement(newSource))
		case oldExists && newExists && !oldSource.Equal(newSource):
			statements = append(statements, updateSourceStatement(newSource))
		case oldExists && !newExists:
			statements = append(statements, deleteSourceNodeStatement(id))
		}
	}

	// 변경된 에피소드 처리
	for id := range dt.episodes {
		entry := dt.episodes[id]
		if !entry.isDirty {
			continue
		}
		oldEpisode, oldExists := prev.Episodes[id]
		newEpisode, newExists := next.Episodes[id]

		switch {
		case !oldExists && newExists:
			statements = append(statements, createEpisodeStatement(newEpisode))
		case oldExists && newExists && !oldEpisode.Equal(newEpisode):
			statements = append(statements, updateEpisodeStatement(newEpisode))
		case oldExists && !newExists:
			statements = append(statements, deleteEpisodeNodeStatement(id))
		}
	}

	// 변경된 엔티티 처리
	for id := range dt.entities {
		entry := dt.entities[id]
		if !entry.isDirty {
			continue
		}
		oldEntity, oldExists := prev.Entities[id]
		newEntity, newExists := next.Entities[id]

		switch {
		case !oldExists && newExists:
			statements = append(statements, createEntityStatement(newEntity))
		case oldExists && newExists && !oldEntity.Equal(newEntity):
			statements = append(statements, updateEntityStatement(newEntity))
		case oldExists && !newExists:
			statements = append(statements, deleteEntityNodeStatement(id))
		}
	}

	// 변경된 팩트 처리
	for id := range dt.facts {
		entry := dt.facts[id]
		if !entry.isDirty {
			continue
		}
		oldFact, oldExists := prev.Facts[id]
		newFact, newExists := next.Facts[id]

		switch {
		case !oldExists && newExists:
			statements = append(statements, createFactStatement(newFact))
		case oldExists && newExists && !stripFactRelationshipFields(oldFact).Equal(stripFactRelationshipFields(newFact)):
			statements = append(statements, updateFactStatement(newFact))
		case oldExists && !newExists:
			statements = append(statements, deleteFactNodeStatement(id))
		}
	}

	// 변경된 리비전 처리 (추가만 가능)
	for _, entry := range dt.factRevisions {
		if entry.isNew {
			statements = append(statements, createFactRevisionStatement(entry.current))
		}
	}

	for _, entry := range dt.entityRevisions {
		if entry.isNew {
			statements = append(statements, createEntityRevisionStatement(entry.current))
		}
	}

	// 변경된 워크마크 처리
	for id := range dt.watermarks {
		entry := dt.watermarks[id]
		if !entry.isDirty {
			continue
		}
		oldWatermark, oldExists := prev.MigrationWatermarks[id]
		newWatermark, newExists := next.MigrationWatermarks[id]

		switch {
		case !oldExists && newExists:
			statements = append(statements, createMigrationWatermarkStatement(newWatermark))
		case oldExists && newExists && !oldWatermark.Equal(newWatermark):
			statements = append(statements, updateMigrationWatermarkStatement(newWatermark))
		}
	}

	return statements
}

// syncTracker는 현재 상태와 트래커를 동기화합니다.
func (dt *dirtyTracker) syncTracker(state persistedState) {
	// 현재 상태의 모든 항목을 트래커에 동기화
	for id, source := range state.Sources {
		if entry, exists := dt.sources[id]; exists {
			if !entry.isDirty {
				entry.current = source
				dt.sources[id] = entry
			}
		} else {
			dt.sources[id] = dirtyEntry[Source]{current: source, original: source}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.sources {
		if _, exists := state.Sources[id]; !exists {
			if !dt.sources[id].isDirty {
				delete(dt.sources, id)
			}
		}
	}

	for id, episode := range state.Episodes {
		if entry, exists := dt.episodes[id]; exists {
			if !entry.isDirty {
				entry.current = episode
				dt.episodes[id] = entry
			}
		} else {
			dt.episodes[id] = dirtyEntry[Episode]{current: episode, original: episode}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.episodes {
		if _, exists := state.Episodes[id]; !exists {
			if !dt.episodes[id].isDirty {
				delete(dt.episodes, id)
			}
		}
	}

	for id, entity := range state.Entities {
		if entry, exists := dt.entities[id]; exists {
			if !entry.isDirty {
				entry.current = entity
				dt.entities[id] = entry
			}
		} else {
			dt.entities[id] = dirtyEntry[Entity]{current: entity, original: entity}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.entities {
		if _, exists := state.Entities[id]; !exists {
			if !dt.entities[id].isDirty {
				delete(dt.entities, id)
			}
		}
	}

	for id, fact := range state.Facts {
		if entry, exists := dt.facts[id]; exists {
			if !entry.isDirty {
				entry.current = fact
				dt.facts[id] = entry
			}
		} else {
			dt.facts[id] = dirtyEntry[Fact]{current: fact, original: fact}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.facts {
		if _, exists := state.Facts[id]; !exists {
			if !dt.facts[id].isDirty {
				delete(dt.facts, id)
			}
		}
	}

	for id, revision := range state.FactRevisions {
		if entry, exists := dt.factRevisions[id]; exists {
			if !entry.isDirty {
				entry.current = revision
				dt.factRevisions[id] = entry
			}
		} else {
			dt.factRevisions[id] = dirtyEntry[FactRevision]{current: revision, original: revision}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.factRevisions {
		if _, exists := state.FactRevisions[id]; !exists {
			if !dt.factRevisions[id].isDirty {
				delete(dt.factRevisions, id)
			}
		}
	}

	for id, revision := range state.EntityRevisions {
		if entry, exists := dt.entityRevisions[id]; exists {
			if !entry.isDirty {
				entry.current = revision
				dt.entityRevisions[id] = entry
			}
		} else {
			dt.entityRevisions[id] = dirtyEntry[EntityRevision]{current: revision, original: revision}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.entityRevisions {
		if _, exists := state.EntityRevisions[id]; !exists {
			if !dt.entityRevisions[id].isDirty {
				delete(dt.entityRevisions, id)
			}
		}
	}

	for id, watermark := range state.MigrationWatermarks {
		if entry, exists := dt.watermarks[id]; exists {
			if !entry.isDirty {
				entry.current = watermark
				dt.watermarks[id] = entry
			}
		} else {
			dt.watermarks[id] = dirtyEntry[MigrationWatermark]{current: watermark, original: watermark}
		}
	}

	// 삭제된 항목 제거
	for id := range dt.watermarks {
		if _, exists := state.MigrationWatermarks[id]; !exists {
			if !dt.watermarks[id].isDirty {
				delete(dt.watermarks, id)
			}
		}
	}
}

// lastSaveTime은 마지막 저장 시간을 추적합니다.
type saveMetadata struct {
	lastSaveTime time.Time
	saveCount    uint64
}

func newSaveMetadata() *saveMetadata {
	return &saveMetadata{}
}

func (m *saveMetadata) recordSave() {
	m.lastSaveTime = time.Now().UTC()
	m.saveCount++
}

func (m *saveMetadata) shouldCompact() bool {
	// 100회 저장마다 컴팩션 제안
	return m.saveCount%100 == 0 && m.saveCount > 0
}
