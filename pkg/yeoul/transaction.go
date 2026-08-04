package yeoul

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Transaction은 트랜잭션을 관리합니다.
type Transaction struct {
	id        string
	store     stateStore
	snapshot  persistedState
	current   persistedState // 현재 변경된 상태
	startTime time.Time
	status    TxStatus
	mu        sync.RWMutex
}

// TxStatus는 트랜잭션 상태를 나타냅니다.
type TxStatus string

const (
	TxStatusActive    TxStatus = "active"
	TxStatusCommitted TxStatus = "committed"
	TxStatusAborted   TxStatus = "aborted"
)

// TxManager는 트랜잭션 매니저입니다.
type TxManager struct {
	store        stateStore
	transactions map[string]*Transaction
	mu           sync.RWMutex
}

// newTxManager는 새로운 트랜잭션 매니저를 생성합니다.
func newTxManager(store stateStore) *TxManager {
	return &TxManager{
		store:        store,
		transactions: make(map[string]*Transaction),
	}
}

// Begin는 새로운 트랜잭션을 시작합니다.
func (tm *TxManager) Begin() (*Transaction, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 현재 상태 스냅샷
	state, err := tm.store.Load()
	if err != nil {
		return nil, err
	}

	tx := &Transaction{
		id:        fmt.Sprintf("tx_%s", uuid.New().String()),
		store:     tm.store,
		snapshot:  *state,
		current:   clonePersistedState(*state),
		startTime: time.Now().UTC(),
		status:    TxStatusActive,
	}

	tm.transactions[tx.id] = tx
	return tx, nil
}

// GetTransaction은 트랜잭션을 반환합니다.
func (tm *TxManager) GetTransaction(id string) (*Transaction, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tx, ok := tm.transactions[id]
	return tx, ok
}

// ListTransactions는 활성 트랜잭션 목록을 반환합니다.
func (tm *TxManager) ListTransactions() []*TransactionInfo {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var infos []*TransactionInfo
	for _, tx := range tm.transactions {
		tx.mu.RLock()
		info := &TransactionInfo{
			ID:        tx.id,
			Status:    string(tx.status),
			StartTime: tx.startTime,
		}
		tx.mu.RUnlock()
		infos = append(infos, info)
	}
	return infos
}

// Cleanup은 완료된 트랜잭션을 정리합니다.
func (tm *TxManager) Cleanup() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for id, tx := range tm.transactions {
		if tx.status != TxStatusActive {
			delete(tm.transactions, id)
		}
	}
}

// Commit는 트랜잭션을 커밋합니다.
func (tx *Transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "commit",
			Message: "transaction is not active",
		}
	}

	// 실제 저장 수행
	if err := tx.store.Save(tx.current); err != nil {
		return &TransactionError{
			Op:      "commit",
			Message: "failed to save state",
			Err:     err,
		}
	}

	tx.status = TxStatusCommitted
	return nil
}

// Abort는 트랜잭션을 중단합니다.
func (tx *Transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "abort",
			Message: "transaction is not active",
		}
	}

	tx.status = TxStatusAborted
	return nil
}

// GetSnapshot은 트랜잭션 시작 시점의 스냅샷을 반환합니다.
func (tx *Transaction) GetSnapshot() persistedState {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	return clonePersistedState(tx.snapshot)
}

// IsActive는 트랜잭션이 활성 상태인지 확인합니다.
func (tx *Transaction) IsActive() bool {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	return tx.status == TxStatusActive
}

// GetID는 트랜잭션 ID를 반환합니다.
func (tx *Transaction) GetID() string {
	return tx.id
}

// GetStartTime는 트랜잭션 시작 시간을 반환합니다.
func (tx *Transaction) GetStartTime() time.Time {
	return tx.startTime
}

// GetDuration은 트랜잭션 실행 시간을 반환합니다.
func (tx *Transaction) GetDuration() time.Duration {
	return time.Since(tx.startTime)
}

// UpdateSource는 소스를 업데이트합니다.
func (tx *Transaction) UpdateSource(source Source) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "update_source",
			Message: "transaction is not active",
		}
	}

	tx.current.Sources[source.ID] = source
	return nil
}

// UpdateEpisode는 에피소드를 업데이트합니다.
func (tx *Transaction) UpdateEpisode(episode Episode) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "update_episode",
			Message: "transaction is not active",
		}
	}

	tx.current.Episodes[episode.ID] = episode
	return nil
}

// UpdateEntity는 엔티티를 업데이트합니다.
func (tx *Transaction) UpdateEntity(entity Entity) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "update_entity",
			Message: "transaction is not active",
		}
	}

	tx.current.Entities[entity.ID] = entity
	return nil
}

// UpdateFact는 팩트를 업데이트합니다.
func (tx *Transaction) UpdateFact(fact Fact) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "update_fact",
			Message: "transaction is not active",
		}
	}

	tx.current.Facts[fact.ID] = fact
	return nil
}

// DeleteSource는 소스를 삭제합니다.
func (tx *Transaction) DeleteSource(id string) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "delete_source",
			Message: "transaction is not active",
		}
	}

	delete(tx.current.Sources, id)
	return nil
}

// DeleteEpisode는 에피소드를 삭제합니다.
func (tx *Transaction) DeleteEpisode(id string) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "delete_episode",
			Message: "transaction is not active",
		}
	}

	delete(tx.current.Episodes, id)
	return nil
}

// DeleteEntity는 엔티티를 삭제합니다.
func (tx *Transaction) DeleteEntity(id string) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "delete_entity",
			Message: "transaction is not active",
		}
	}

	delete(tx.current.Entities, id)
	return nil
}

// DeleteFact는 팩트를 삭제합니다.
func (tx *Transaction) DeleteFact(id string) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.status != TxStatusActive {
		return &TransactionError{
			Op:      "delete_fact",
			Message: "transaction is not active",
		}
	}

	delete(tx.current.Facts, id)
	return nil
}

// GetCurrentState는 현재 트랜잭션의 상태를 반환합니다.
func (tx *Transaction) GetCurrentState() persistedState {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	return clonePersistedState(tx.current)
}

// TransactionInfo는 트랜잭션 정보를 나타냅니다.
type TransactionInfo struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
}

// TransactionManager는 트랜잭션 관리를 위한 인터페이스입니다.
type TransactionManager interface {
	Begin() (*Transaction, error)
	GetTransaction(id string) (*Transaction, bool)
	ListTransactions() []*TransactionInfo
	Cleanup()
}

// TxOptions는 트랜잭션 옵션을 나타냅니다.
type TxOptions struct {
	ReadOnly bool          `json:"read_only"`
	Timeout  time.Duration `json:"timeout"`
}

// DefaultTxOptions는 기본 트랜잭션 옵션을 반환합니다.
func DefaultTxOptions() TxOptions {
	return TxOptions{
		ReadOnly: false,
		Timeout:  30 * time.Second,
	}
}

// WithTx는 트랜잭션 내에서 작업을 수행합니다.
func WithTx(tm *TxManager, fn func(tx *Transaction) error) error {
	tx, err := tm.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if tx.IsActive() {
			tx.Abort()
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

// TxError는 트랜잭션 에러를 나타냅니다.
// TransactionError와 동일한 타입으로, 하위 호환성을 위해 별칭으로 정의합니다.
type TxError = TransactionError
