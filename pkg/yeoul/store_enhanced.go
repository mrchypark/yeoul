package yeoul

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// enhancedStore는 개선된 저장소입니다.
type enhancedStore struct {
	cfg          Config
	baseStore    stateStore
	dirtyTracker *dirtyTracker
	saveMeta     *saveMetadata
	fileLock     *fileLock
	mu           sync.RWMutex
	loaded       bool
	lastState    persistedState
}

// newEnhancedStore는 개선된 저장소를 생성합니다.
func newEnhancedStore(cfg Config) (*enhancedStore, error) {
	baseStore, err := newLadybugStore(cfg)
	if err != nil {
		return nil, err
	}

	store := &enhancedStore{
		cfg:          cfg,
		baseStore:    baseStore,
		dirtyTracker: newDirtyTracker(),
		saveMeta:     newSaveMetadata(),
		fileLock:     newFileLock(cfg.DatabasePath),
	}

	return store, nil
}

// Load는 상태를 로드합니다.
func (s *enhancedStore) Load() (*persistedState, error) {
	state, err := s.baseStore.Load()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastState = clonePersistedState(*state)
	s.dirtyTracker.initFromState(*state)
	s.loaded = true

	return state, nil
}

// Save는 상태를 저장합니다 (최적화된 방식).
func (s *enhancedStore) Save(state persistedState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 읽기 전용 모드에서는 저장 불가
	if s.cfg.ReadOnly {
		return fmt.Errorf("cannot save in read-only mode")
	}

	// 파일 락 획득
	if err := s.fileLock.Lock(); err != nil {
		return err
	}
	defer s.fileLock.Unlock()

	// 변경 사항 확인
	if s.loaded {
		_, hasChanges := s.dirtyTracker.getDirtyState(state)
		if !hasChanges {
			// 변경 사항 없음 - 메타데이터만 업데이트
			s.saveMeta.recordSave()
			return nil
		}
	}

	// 상태 저장 (ladybugStore가 delta 계산을 수행)
	if err := s.baseStore.Save(state); err != nil {
		return err
	}

	// 상태 업데이트
	s.lastState = clonePersistedState(state)
	s.dirtyTracker.initFromState(state)
	s.loaded = true
	s.saveMeta.recordSave()

	// 컴팩션 제안 확인
	if s.saveMeta.shouldCompact() {
		fmt.Fprintf(os.Stderr, "info: consider running compaction after %d saves\n", s.saveMeta.saveCount)
	}

	return nil
}

// Close는 저장소를 닫습니다.
func (s *enhancedStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.baseStore != nil {
		return s.baseStore.Close()
	}
	return nil
}

// GetLastSaveTime은 마지막 저장 시간을 반환합니다.
func (s *enhancedStore) GetLastSaveTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveMeta.lastSaveTime
}

// GetSaveCount는 저장 횟수를 반환합니다.
func (s *enhancedStore) GetSaveCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveMeta.saveCount
}

// NeedsCompaction은 컴팩션이 필요한지 확인합니다.
func (s *enhancedStore) NeedsCompaction() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveMeta.shouldCompact()
}

// GetDatabasePath는 데이터베이스 경로를 반환합니다.
func (s *enhancedStore) GetDatabasePath() string {
	return s.cfg.DatabasePath
}

// GetStats는 저장소 통계를 반환합니다.
func (s *enhancedStore) GetStats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StoreStats{
		DatabasePath: s.cfg.DatabasePath,
		SaveCount:    s.saveMeta.saveCount,
		LastSaveTime: s.saveMeta.lastSaveTime,
		Loaded:       s.loaded,
		ReadOnly:     s.cfg.ReadOnly,
	}
}

// StoreStats는 저장소 통계를 나타냅니다.
type StoreStats struct {
	DatabasePath string    `json:"database_path"`
	SaveCount    uint64    `json:"save_count"`
	LastSaveTime time.Time `json:"last_save_time"`
	Loaded       bool      `json:"loaded"`
	ReadOnly     bool      `json:"read_only"`
}

// enhancedMemoryStore는 개선된 인메모리 저장소입니다.
type enhancedMemoryStore struct {
	state persistedState
	mu    sync.RWMutex
}

func newEnhancedMemoryStore() *enhancedMemoryStore {
	return &enhancedMemoryStore{
		state: emptyPersistedState(),
	}
}

func (s *enhancedMemoryStore) Load() (*persistedState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cloned := clonePersistedState(s.state)
	return &cloned, nil
}

func (s *enhancedMemoryStore) Save(state persistedState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = clonePersistedState(state)
	return nil
}

func (s *enhancedMemoryStore) Close() error {
	return nil
}

func (s *enhancedMemoryStore) GetStats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return StoreStats{
		DatabasePath: "memory",
		SaveCount:    0,
		LastSaveTime: time.Time{},
		Loaded:       true,
		ReadOnly:     false,
	}
}

// openEnhancedStore는 개선된 저장소를 엽니다.
func openEnhancedStore(cfg Config) (stateStore, error) {
	if cfg.InMemory {
		return newEnhancedMemoryStore(), nil
	}

	return newEnhancedStore(cfg)
}

// CompactionResult는 컴팩션 결과를 나타냅니다.
type CompactionResult struct {
	Version            int  `json:"version"`
	Compacted          bool `json:"compacted"`
	RemovedRevisions   int  `json:"removed_revisions"`
	RemainingRevisions int  `json:"remaining_revisions"`
}

// CompactionOptions는 컴팩션 옵션을 나타냅니다.
type CompactionOptions struct {
	MaxRevisionsPerFact   int  `json:"max_revisions_per_fact"`
	MaxRevisionsPerEntity int  `json:"max_revisions_per_entity"`
	DryRun                bool `json:"dry_run"`
}

// DefaultCompactionOptions는 기본 컴팩션 옵션을 반환합니다.
func DefaultCompactionOptions() CompactionOptions {
	return CompactionOptions{
		MaxRevisionsPerFact:   10,
		MaxRevisionsPerEntity: 10,
		DryRun:                true,
	}
}

