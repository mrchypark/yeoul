package yeoul

import (
	"fmt"
	"sync"
	"time"
)

// Transaction은 트랜잭션을 관리합니다.
type Transaction struct {
	id        string
	store     stateStore
	snapshot  persistedState
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
		id:        fmt.Sprintf("tx_%d", time.Now().UnixNano()),
		store:     tm.store,
		snapshot:  *state,
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
		infos = append(infos, &TransactionInfo{
			ID:        tx.id,
			Status:    string(tx.status),
			StartTime: tx.startTime,
		})
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

	return tx.snapshot
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
	ReadOnly       bool           `json:"read_only"`
	Timeout        time.Duration  `json:"timeout"`
	IsolationLevel IsolationLevel `json:"isolation_level"`
}

// IsolationLevel은 격리 수준을 나타냅니다.
type IsolationLevel string

const (
	IsolationLevelReadUncommitted IsolationLevel = "read_uncommitted"
	IsolationLevelReadCommitted   IsolationLevel = "read_committed"
	IsolationLevelRepeatableRead  IsolationLevel = "repeatable_read"
	IsolationLevelSerializable    IsolationLevel = "serializable"
)

// DefaultTxOptions는 기본 트랜잭션 옵션을 반환합니다.
func DefaultTxOptions() TxOptions {
	return TxOptions{
		ReadOnly:       false,
		Timeout:        30 * time.Second,
		IsolationLevel: IsolationLevelReadCommitted,
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
type TxError struct {
	Op      string
	Message string
	Err     error
}

func (e *TxError) Error() string {
	return fmt.Sprintf("transaction error: %s: %s", e.Op, e.Message)
}

func (e *TxError) Unwrap() error {
	return e.Err
}
