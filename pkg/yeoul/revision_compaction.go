package yeoul

import (
	"fmt"
	"sort"
)

// RevisionCompactor는 리비전 컴팩션을 관리합니다.
type RevisionCompactor struct {
	factRevisionLimit   int
	entityRevisionLimit int
}

// NewRevisionCompactor는 새로운 리비전 컴팩터를 생성합니다.
func NewRevisionCompactor(factLimit, entityLimit int) *RevisionCompactor {
	return &RevisionCompactor{
		factRevisionLimit:   factLimit,
		entityRevisionLimit: entityLimit,
	}
}

// CompactFactRevisions는 팩트 리비전을 컴팩션합니다.
func (rc *RevisionCompactor) CompactFactRevisions(state *persistedState) CompactionResult {
	result := CompactionResult{
		DatabasePath: fmt.Sprintf("%d", state.Version),
	}

	// 팩트별 리비전 그룹화
	revisionsByFact := make(map[string][]FactRevision)
	for _, revision := range state.FactRevisions {
		revisionsByFact[revision.FactID] = append(revisionsByFact[revision.FactID], revision)
	}

	// 각 팩트별로 리비전 수 확인 및 컴팩션
	for factID, revisions := range revisionsByFact {
		if len(revisions) <= rc.factRevisionLimit {
			continue
		}

		// 리비전 정렬 (최신 순)
		sort.Slice(revisions, func(i, j int) bool {
			return revisions[i].TxTime.After(revisions[j].TxTime)
		})

		// 오래된 리비전 제거
		toRemove := revisions[rc.factRevisionLimit:]
		for _, revision := range toRemove {
			delete(state.FactRevisions, revision.ID)
			result.RemovedRevisions++
		}

		result.RemainingRevisions += rc.factRevisionLimit
		_ = factID
	}

	result.Compacted = result.RemovedRevisions > 0
	return result
}

// CompactEntityRevisions는 엔티티 리비전을 컴팩션합니다.
func (rc *RevisionCompactor) CompactEntityRevisions(state *persistedState) CompactionResult {
	result := CompactionResult{}

	// 엔티티별 리비전 그룹화
	revisionsByEntity := make(map[string][]EntityRevision)
	for _, revision := range state.EntityRevisions {
		revisionsByEntity[revision.EntityID] = append(revisionsByEntity[revision.EntityID], revision)
	}

	// 각 엔티티별로 리비전 수 확인 및 컴팩션
	for entityID, revisions := range revisionsByEntity {
		if len(revisions) <= rc.entityRevisionLimit {
			continue
		}

		// 리비전 정렬 (최신 순)
		sort.Slice(revisions, func(i, j int) bool {
			return revisions[i].TxTime.After(revisions[j].TxTime)
		})

		// 오래된 리비전 제거
		toRemove := revisions[rc.entityRevisionLimit:]
		for _, revision := range toRemove {
			delete(state.EntityRevisions, revision.ID)
			result.RemovedRevisions++
		}

		result.RemainingRevisions += rc.entityRevisionLimit
		_ = entityID
	}

	result.Compacted = result.RemovedRevisions > 0
	return result
}

// CompactAll은 모든 리비전을 컴팩션합니다.
func (rc *RevisionCompactor) CompactAll(state *persistedState) []CompactionResult {
	results := make([]CompactionResult, 0, 2)

	factResult := rc.CompactFactRevisions(state)
	if factResult.Compacted {
		results = append(results, factResult)
	}

	entityResult := rc.CompactEntityRevisions(state)
	if entityResult.Compacted {
		results = append(results, entityResult)
	}

	return results
}

// NeedsCompaction은 컴팩션이 필요한지 확인합니다.
func (rc *RevisionCompactor) NeedsCompaction(state persistedState) bool {
	// 팩트 리비전 확인
	factRevisions := make(map[string]int)
	for _, revision := range state.FactRevisions {
		factRevisions[revision.FactID]++
	}

	for _, count := range factRevisions {
		if count > rc.factRevisionLimit {
			return true
		}
	}

	// 엔티티 리비전 확인
	entityRevisions := make(map[string]int)
	for _, revision := range state.EntityRevisions {
		entityRevisions[revision.EntityID]++
	}

	for _, count := range entityRevisions {
		if count > rc.entityRevisionLimit {
			return true
		}
	}

	return false
}

// GetRevisionStats는 리비전 통계를 반환합니다.
func (rc *RevisionCompactor) GetRevisionStats(state persistedState) RevisionStats {
	stats := RevisionStats{
		TotalFactRevisions:      len(state.FactRevisions),
		TotalEntityRevisions:    len(state.EntityRevisions),
		FactRevisionsByFact:     make(map[string]int),
		EntityRevisionsByEntity: make(map[string]int),
	}

	for _, revision := range state.FactRevisions {
		stats.FactRevisionsByFact[revision.FactID]++
	}

	for _, revision := range state.EntityRevisions {
		stats.EntityRevisionsByEntity[revision.EntityID]++
	}

	// 가장 많은 리비전을 가진 팩트/엔티티 찾기
	for factID, count := range stats.FactRevisionsByFact {
		if count > stats.MaxFactRevisions {
			stats.MaxFactRevisions = count
			stats.MaxFactRevisionsID = factID
		}
	}

	for entityID, count := range stats.EntityRevisionsByEntity {
		if count > stats.MaxEntityRevisions {
			stats.MaxEntityRevisions = count
			stats.MaxEntityRevisionsID = entityID
		}
	}

	return stats
}

// RevisionStats는 리비전 통계를 나타냅니다.
type RevisionStats struct {
	TotalFactRevisions      int            `json:"total_fact_revisions"`
	TotalEntityRevisions    int            `json:"total_entity_revisions"`
	FactRevisionsByFact     map[string]int `json:"fact_revisions_by_fact"`
	EntityRevisionsByEntity map[string]int `json:"entity_revisions_by_entity"`
	MaxFactRevisions        int            `json:"max_fact_revisions"`
	MaxFactRevisionsID      string         `json:"max_fact_revisions_id"`
	MaxEntityRevisions      int            `json:"max_entity_revisions"`
	MaxEntityRevisionsID    string         `json:"max_entity_revisions_id"`
}

// String은 CompactionResult의 문자열 표현을 반환합니다.
func (r CompactionResult) String() string {
	return "compacted"
}
