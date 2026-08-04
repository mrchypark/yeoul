package yeoul

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// fileLock은 프로세스 간 파일 락을 관리합니다.
type fileLock struct {
	path   string
	file   *os.File
	locked bool
	mu     sync.RWMutex
}

// newFileLock은 새로운 파일 락을 생성합니다.
func newFileLock(path string) *fileLock {
	return &fileLock{
		path: filepath.Dir(path) + "/.yeoul.lock",
	}
}

// writeLockInfo는 락 정보를 파일에 기록합니다.
func (fl *fileLock) writeLockInfo() {
	if fl.file == nil {
		return
	}

	data := []byte("pid: " + strconv.Itoa(os.Getpid()) + "\n")
	// 에러 무시 - 락 정보 기록 실패는 치명적이지 않음
	_, _ = fl.file.Write(data)
}

// isLockStale은 락이 오래되었는지 확인합니다.
func (fl *fileLock) isLockStale() bool {
	info, err := os.Stat(fl.path)
	if err != nil {
		return false
	}

	// 5분 이상 오래된 락은 stale로 간주
	return time.Since(info.ModTime()) > 5*time.Minute
}

// removeStaleLock은 오래된 락을 제거합니다.
func (fl *fileLock) removeStaleLock() {
	os.Remove(fl.path)
}

// LockError는 락 관련 에러를 나타냅니다.
type LockError struct {
	Path    string
	Message string
}

func (e *LockError) Error() string {
	return "lock error for " + e.Path + ": " + e.Message
}

// IsLocked는 락이 획득되었는지 확인합니다.
func (fl *fileLock) IsLocked() bool {
	fl.mu.RLock()
	defer fl.mu.RUnlock()
	return fl.locked
}

// Path는 락 파일 경로를 반환합니다.
func (fl *fileLock) Path() string {
	return fl.path
}
