package yeoul

import (
	"fmt"
	"regexp"
	"strings"
)

// validIDPattern은 ID 유효성 검사용 정규식입니다.
var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-_\.]+$`)

// IndexManager는 인덱스를 관리합니다.
type IndexManager struct {
	store *ladybugStore
}

// newIndexManager는 새로운 인덱스 매니저를 생성합니다.
func newIndexManager(store *ladybugStore) *IndexManager {
	return &IndexManager{store: store}
}

// EnsureIndexes는 필요한 인덱스를 생성합니다.
func (im *IndexManager) EnsureIndexes() error {
	indexes := []string{
		// Source 인덱스
		"CREATE INDEX IF NOT EXISTS FOR (s:Source) ON (s.id)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Source) ON (s.space_id)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Source) ON (s.kind)",

		// Episode 인덱스
		"CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.space_id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.kind)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.source_id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.group_id)",

		// Entity 인덱스
		"CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.space_id)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.type)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.namespace)",

		// Fact 인덱스
		"CREATE INDEX IF NOT EXISTS FOR (f:Fact) ON (f.id)",
		"CREATE INDEX IF NOT EXISTS FOR (f:Fact) ON (f.space_id)",
		"CREATE INDEX IF NOT EXISTS FOR (f:Fact) ON (f.predicate)",
		"CREATE INDEX IF NOT EXISTS FOR (f:Fact) ON (f.status)",

		// Revision 인덱스
		"CREATE INDEX IF NOT EXISTS FOR (r:FactRevision) ON (r.id)",
		"CREATE INDEX IF NOT EXISTS FOR (r:FactRevision) ON (r.fact_id)",
		"CREATE INDEX IF NOT EXISTS FOR (r:EntityRevision) ON (r.id)",
		"CREATE INDEX IF NOT EXISTS FOR (r:EntityRevision) ON (r.entity_id)",
	}

	var lastErr error
	for _, index := range indexes {
		if err := im.store.exec(index); err != nil {
			lastErr = fmt.Errorf("failed to create index: %w", err)
		}
	}

	return lastErr
}

// DropIndexes는 모든 인덱스를 제거합니다.
func (im *IndexManager) DropIndexes() error {
	indexes := []string{
		"DROP INDEX IF EXISTS FOR (s:Source) ON (s.id)",
		"DROP INDEX IF EXISTS FOR (s:Source) ON (s.space_id)",
		"DROP INDEX IF EXISTS FOR (s:Source) ON (s.kind)",
		"DROP INDEX IF EXISTS FOR (e:Episode) ON (e.id)",
		"DROP INDEX IF EXISTS FOR (e:Episode) ON (e.space_id)",
		"DROP INDEX IF EXISTS FOR (e:Episode) ON (e.kind)",
		"DROP INDEX IF EXISTS FOR (e:Episode) ON (e.source_id)",
		"DROP INDEX IF EXISTS FOR (e:Episode) ON (e.group_id)",
		"DROP INDEX IF EXISTS FOR (e:Entity) ON (e.id)",
		"DROP INDEX IF EXISTS FOR (e:Entity) ON (e.space_id)",
		"DROP INDEX IF EXISTS FOR (e:Entity) ON (e.type)",
		"DROP INDEX IF EXISTS FOR (e:Entity) ON (e.namespace)",
		"DROP INDEX IF EXISTS FOR (f:Fact) ON (f.id)",
		"DROP INDEX IF EXISTS FOR (f:Fact) ON (f.space_id)",
		"DROP INDEX IF EXISTS FOR (f:Fact) ON (f.predicate)",
		"DROP INDEX IF EXISTS FOR (f:Fact) ON (f.status)",
		"DROP INDEX IF EXISTS FOR (r:FactRevision) ON (r.id)",
		"DROP INDEX IF EXISTS FOR (r:FactRevision) ON (r.fact_id)",
		"DROP INDEX IF EXISTS FOR (r:EntityRevision) ON (r.id)",
		"DROP INDEX IF EXISTS FOR (r:EntityRevision) ON (r.entity_id)",
	}

	var lastErr error
	for _, index := range indexes {
		if err := im.store.exec(index); err != nil {
			lastErr = fmt.Errorf("failed to drop index: %w", err)
		}
	}

	return lastErr
}

// ListIndexes는 현재 인덱스 목록을 반환합니다.
func (im *IndexManager) ListIndexes() ([]IndexInfo, error) {
	result, err := im.store.store.Query("CALL show_indexes() RETURN *")
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var indexes []IndexInfo
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			continue
		}

		values, err := tuple.GetAsSlice()
		if err != nil || len(values) < 3 {
			continue
		}

		index := IndexInfo{
			Name:  fmt.Sprint(values[0]),
			Type:  fmt.Sprint(values[1]),
			Table: fmt.Sprint(values[2]),
		}
		indexes = append(indexes, index)
	}

	return indexes, nil
}

// IndexInfo는 인덱스 정보를 나타냅니다.
type IndexInfo struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Table string `json:"table"`
}

// RebuildIndexes는 인덱스를 재건합니다.
func (im *IndexManager) RebuildIndexes() error {
	// 먼저 기존 인덱스 제거
	if err := im.DropIndexes(); err != nil {
		return err
	}

	// 그 다음 새 인덱스 생성
	return im.EnsureIndexes()
}

// AnalyzeStatistics는 통계 정보를 분석합니다.
func (im *IndexManager) AnalyzeStatistics() error {
	queries := []string{
		"MATCH (s:Source) RETURN count(s) AS source_count",
		"MATCH (e:Episode) RETURN count(e) AS episode_count",
		"MATCH (e:Entity) RETURN count(e) AS entity_count",
		"MATCH (f:Fact) RETURN count(f) AS fact_count",
		"MATCH (r:FactRevision) RETURN count(r) AS fact_revision_count",
		"MATCH (r:EntityRevision) RETURN count(r) AS entity_revision_count",
	}

	var lastErr error
	for _, query := range queries {
		if err := im.store.exec(query); err != nil {
			lastErr = fmt.Errorf("failed to analyze statistics: %w", err)
		}
	}

	return lastErr
}

// GetIndexStats는 인덱스 통계를 반환합니다.
func (im *IndexManager) GetIndexStats() (map[string]int, error) {
	stats := make(map[string]int)

	queries := map[string]string{
		"sources":          "MATCH (s:Source) RETURN count(s)",
		"episodes":         "MATCH (e:Episode) RETURN count(e)",
		"entities":         "MATCH (e:Entity) RETURN count(e)",
		"facts":            "MATCH (f:Fact) RETURN count(f)",
		"fact_revisions":   "MATCH (r:FactRevision) RETURN count(r)",
		"entity_revisions": "MATCH (r:EntityRevision) RETURN count(r)",
	}

	for key, query := range queries {
		result, err := im.store.store.Query(query)
		if err != nil {
			continue
		}

		if result.HasNext() {
			tuple, err := result.Next()
			if err != nil {
				result.Close()
				continue
			}

			values, err := tuple.GetAsSlice()
			if err != nil || len(values) == 0 {
				result.Close()
				continue
			}

			if count, ok := values[0].(int64); ok {
				stats[key] = int(count)
			}
		}
		result.Close()
	}

	return stats, nil
}

// sanitizeID는 Cypher 인젝션을 방지하기 위해 ID 문자열을 검증하고 정리합니다.
func sanitizeID(id string) (string, error) {
	if len(id) == 0 {
		return "", fmt.Errorf("empty ID")
	}
	// 영숫자, 하이픈, 언더스코어, 점만 허용 (최대 255자)
	if len(id) > 255 {
		return "", fmt.Errorf("ID too long: %d characters", len(id))
	}
	if !validIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid ID format: %s", id)
	}
	return id, nil
}

// EnsureSpaceIndexes는 특정 공간(space)에 대한 인덱스를 생성합니다.
func (im *IndexManager) EnsureSpaceIndexes(spaceID string) error {
	if strings.TrimSpace(spaceID) == "" {
		return nil
	}

	// spaceID 검증
	sanitizedID, err := sanitizeID(spaceID)
	if err != nil {
		return fmt.Errorf("invalid space ID: %w", err)
	}

	// 검증된 ID는 영숫자, 하이픈, 언더스코어, 점만 포함하므로 안전한 문자열 보간 사용
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS FOR (s:Source) ON (s.space_id) WHERE s.space_id = '%s'", sanitizedID),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS FOR (e:Episode) ON (e.space_id) WHERE e.space_id = '%s'", sanitizedID),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS FOR (e:Entity) ON (e.space_id) WHERE e.space_id = '%s'", sanitizedID),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS FOR (f:Fact) ON (f.space_id) WHERE f.space_id = '%s'", sanitizedID),
	}

	for _, index := range indexes {
		if err := im.store.exec(index); err != nil {
			fmt.Printf("warning: failed to create space index for %s: %v\n", sanitizedID, err)
		}
	}

	return nil
}
