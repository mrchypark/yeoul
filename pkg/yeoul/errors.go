package yeoul

import (
	"fmt"
	"time"
)

type ErrorCode string

const (
	ErrConfigInvalid     ErrorCode = "YEOUL_CONFIG_INVALID"
	ErrInputInvalid      ErrorCode = "YEOUL_INPUT_INVALID"
	ErrEntityNotFound    ErrorCode = "YEOUL_ENTITY_NOT_FOUND"
	ErrFactNotFound      ErrorCode = "YEOUL_FACT_NOT_FOUND"
	ErrSourceNotFound    ErrorCode = "YEOUL_SOURCE_NOT_FOUND"
	ErrLifecycleInvalid  ErrorCode = "YEOUL_LIFECYCLE_INVALID"
	ErrQueryFailed       ErrorCode = "YEOUL_QUERY_FAILED"
	ErrNotSupported      ErrorCode = "YEOUL_NOT_SUPPORTED"
	ErrStorageFailed     ErrorCode = "YEOUL_STORAGE_FAILED"
	ErrLockFailed        ErrorCode = "YEOUL_LOCK_FAILED"
	ErrCompactionFailed  ErrorCode = "YEOUL_COMPACTION_FAILED"
	ErrTransactionFailed ErrorCode = "YEOUL_TRANSACTION_FAILED"
)

// Error is the structured error shape surfaced by the public Go API.
type Error struct {
	Code      ErrorCode
	Message   string
	Details   map[string]any
	Cause     error
	Timestamp time.Time
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func errorf(code ErrorCode, message string, details map[string]any, cause error) error {
	return &Error{
		Code:      code,
		Message:   message,
		Details:   details,
		Cause:     cause,
		Timestamp: time.Now().UTC(),
	}
}

// IsNotFoundError는 에러가 NotFound 유형인지 확인합니다.
func IsNotFoundError(err error) bool {
	if e, ok := err.(*Error); ok {
		switch e.Code {
		case ErrEntityNotFound, ErrFactNotFound, ErrSourceNotFound:
			return true
		}
	}
	return false
}

// IsStorageError는 에러가 Storage 유형인지 확인합니다.
func IsStorageError(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrStorageFailed
	}
	return false
}

// IsLockError는 에러가 Lock 유형인지 확인합니다.
func IsLockError(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrLockFailed
	}
	return false
}

// IsCompactionError는 에러가 Compaction 유형인지 확인합니다.
func IsCompactionError(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrCompactionFailed
	}
	return false
}

// IsTransactionError는 에러가 Transaction 유형인지 확인합니다.
func IsTransactionError(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrTransactionFailed
	}
	return false
}

// GetErrorDetails는 에러의 상세 정보를 반환합니다.
func GetErrorDetails(err error) map[string]any {
	if e, ok := err.(*Error); ok {
		return e.Details
	}
	return nil
}

// GetErrorCode는 에러 코드를 반환합니다.
func GetErrorCode(err error) ErrorCode {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return ""
}

// StorageError는 저장소 관련 에러를 나타냅니다.
type StorageError struct {
	Op      string
	Path    string
	Message string
	Err     error
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("storage error: %s %s: %s", e.Op, e.Path, e.Message)
}

func (e *StorageError) Unwrap() error {
	return e.Err
}

// TransactionError는 트랜잭션 관련 에러를 나타냅니다.
type TransactionError struct {
	Op      string
	Message string
	Err     error
}

func (e *TransactionError) Error() string {
	return fmt.Sprintf("transaction error: %s: %s", e.Op, e.Message)
}

func (e *TransactionError) Unwrap() error {
	return e.Err
}
