package yeoul

import (
	"testing"
	"time"
)

func TestDirtyTrackerBasicOperations(t *testing.T) {
	tracker := newDirtyTracker()
	
	// 초기 상태 설정
	state := emptyPersistedState()
	state.Sequence = 1
	state.Sources["src_001"] = Source{ID: "src_001", Kind: "test"}
	state.Episodes["ep_001"] = Episode{ID: "ep_001", Kind: "note"}
	state.Entities["ent_001"] = Entity{ID: "ent_001", Type: "Project"}
	state.Facts["fact_001"] = Fact{ID: "fact_001", Predicate: "USES"}
	
	tracker.initFromState(state)
	
	// 변경 추적
	newState := state
	newState.Sources["src_001"] = Source{ID: "src_001", Kind: "updated"}
	newState.Episodes["ep_002"] = Episode{ID: "ep_002", Kind: "new_note"}
	
	tracker.markSource("src_001", newState.Sources["src_001"])
	tracker.markEpisode("ep_002", newState.Episodes["ep_002"])
	
	// 변경된 항목만 포함된 상태 가져오기
	dirtyState, hasChanges := tracker.getDirtyState(newState)
	
	if !hasChanges {
		t.Error("expected changes, got none")
	}
	
	if len(dirtyState.Sources) == 0 {
		t.Error("expected dirty sources, got none")
	}
}

func TestDirtyTrackerDeleteOperations(t *testing.T) {
	tracker := newDirtyTracker()
	
	// 초기 상태 설정
	state := emptyPersistedState()
	state.Sources["src_001"] = Source{ID: "src_001", Kind: "test"}
	state.Episodes["ep_001"] = Episode{ID: "ep_001", Kind: "note"}
	state.Entities["ent_001"] = Entity{ID: "ent_001", Type: "Project"}
	state.Facts["fact_001"] = Fact{ID: "fact_001", Predicate: "USES"}
	
	tracker.initFromState(state)
	
	// 삭제 추적
	tracker.markSourceDeleted("src_001")
	tracker.markEpisodeDeleted("ep_001")
	tracker.markEntityDeleted("ent_001")
	tracker.markFactDeleted("fact_001")
	
	// 변경된 항목만 포함된 상태 가져오기
	newState := emptyPersistedState()
	_, hasChanges := tracker.getDirtyState(newState)
	
	if !hasChanges {
		t.Error("expected changes from deletions, got none")
	}
}

func TestFileLockBasicOperations(t *testing.T) {
	lock := newFileLock("/tmp/test_yeoul.lock")
	
	// 락 획득 시도
	err := lock.Lock()
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	
	if !lock.IsLocked() {
		t.Error("expected lock to be acquired")
	}
	
	// 락 해제
	err = lock.Unlock()
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}
	
	if lock.IsLocked() {
		t.Error("expected lock to be released")
	}
}

func TestStoreEnhancedBasicOperations(t *testing.T) {
	store := newEnhancedMemoryStore()
	
	// 초기 상태 로드
	state, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	
	if state == nil {
		t.Error("expected non-nil state")
	}
	
	// 상태 저장
	newState := emptyPersistedState()
	newState.Sequence = 1
	newState.Sources["src_001"] = Source{ID: "src_001", Kind: "test"}
	
	err = store.Save(newState)
	if err != nil {
		t.Fatalf("failed to save state: %v", err)
	}
	
	// 다시 로드하여 확인
	loadedState, err := store.Load()
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}
	
	if loadedState.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", loadedState.Sequence)
	}
}

func TestRevisionCompactorBasicOperations(t *testing.T) {
	compactor := NewRevisionCompactor(5, 5)
	
	// 테스트 상태 생성
	state := emptyPersistedState()
	
	// 팩트 리비전 생성
	for i := 0; i < 10; i++ {
		revision := FactRevision{
			ID:       "factrev_" + string(rune('a'+i)),
			FactID:   "fact_001",
			TxTime:   time.Now().Add(time.Duration(i) * time.Minute),
			Predicate: "USES",
		}
		state.FactRevisions[revision.ID] = revision
	}
	
	// 컴팩션 수행
	result := compactor.CompactFactRevisions(&state)
	
	if !result.Compacted {
		t.Error("expected compaction to be performed")
	}
	
	if result.RemovedRevisions == 0 {
		t.Error("expected some revisions to be removed")
	}
}

func TestTransactionManagerBasicOperations(t *testing.T) {
	store := newEnhancedMemoryStore()
	txManager := newTxManager(store)
	
	// 트랜잭션 시작
	tx, err := txManager.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	
	if !tx.IsActive() {
		t.Error("expected transaction to be active")
	}
	
	// 트랜잭션 커밋
	err = tx.Commit()
	if err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}
	
	if tx.IsActive() {
		t.Error("expected transaction to be committed")
	}
}

func TestTransactionManagerAbort(t *testing.T) {
	store := newEnhancedMemoryStore()
	txManager := newTxManager(store)
	
	// 트랜잭션 시작
	tx, err := txManager.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	
	// 트랜잭션 중단
	err = tx.Abort()
	if err != nil {
		t.Fatalf("failed to abort transaction: %v", err)
	}
	
	if tx.IsActive() {
		t.Error("expected transaction to be aborted")
	}
}

func TestErrorTypes(t *testing.T) {
	// NotFound 에러 테스트
	notFoundErr := errorf(ErrEntityNotFound, "entity not found", nil, nil)
	if !IsNotFoundError(notFoundErr) {
		t.Error("expected NotFoundError")
	}
	
	// Storage 에러 테스트
	storageErr := errorf(ErrStorageFailed, "storage failed", nil, nil)
	if !IsStorageError(storageErr) {
		t.Error("expected StorageError")
	}
	
	// Lock 에러 테스트
	lockErr := errorf(ErrLockFailed, "lock failed", nil, nil)
	if !IsLockError(lockErr) {
		t.Error("expected LockError")
	}
}

func TestStoreStats(t *testing.T) {
	store := newEnhancedMemoryStore()
	
	// 통계 조회
	stats := store.GetStats()
	
	if stats.SaveCount != 0 {
		t.Errorf("expected save count 0, got %d", stats.SaveCount)
	}
	
	if stats.Loaded {
		t.Error("expected not loaded initially")
	}
}

func TestCompactionOptions(t *testing.T) {
	opts := DefaultCompactionOptions()
	
	if opts.MaxRevisionsPerFact != 10 {
		t.Errorf("expected 10, got %d", opts.MaxRevisionsPerFact)
	}
	
	if opts.MaxRevisionsPerEntity != 10 {
		t.Errorf("expected 10, got %d", opts.MaxRevisionsPerEntity)
	}
	
	if !opts.DryRun {
		t.Error("expected dry run to be true")
	}
}

func TestIndexManager(t *testing.T) {
	// IndexManager 테스트는 Ladybug 스토리지가 필요하므로
	// 인메모리 테스트로 스킵
	t.Skip("requires Ladybug storage")
}
