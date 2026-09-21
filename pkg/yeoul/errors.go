package yeoul

import (
	"errors"
	"fmt"
	"time"
)

type ErrorCode string

const (
	ErrConfigInvalid    ErrorCode = "YEOUL_CONFIG_INVALID"
	ErrInputInvalid     ErrorCode = "YEOUL_INPUT_INVALID"
	ErrEntityNotFound   ErrorCode = "YEOUL_ENTITY_NOT_FOUND"
	ErrFactNotFound     ErrorCode = "YEOUL_FACT_NOT_FOUND"
	ErrSourceNotFound   ErrorCode = "YEOUL_SOURCE_NOT_FOUND"
	ErrLifecycleInvalid ErrorCode = "YEOUL_LIFECYCLE_INVALID"
	ErrFactConflict     ErrorCode = "YEOUL_FACT_CONFLICT"
	ErrQueryFailed      ErrorCode = "YEOUL_QUERY_FAILED"
	ErrNotSupported     ErrorCode = "YEOUL_NOT_SUPPORTED"
	ErrStorageFailed    ErrorCode = "YEOUL_STORAGE_FAILED"
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

// unwrapYeoulError는 에러 체인에서 Error 타입을 추출합니다.
func unwrapYeoulError(err error) *Error {
	var yeoulErr *Error
	if errors.As(err, &yeoulErr) {
		return yeoulErr
	}
	return nil
}
