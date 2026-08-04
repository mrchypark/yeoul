package yeoul

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
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

// Lock은 파일 락을 획득합니다.
func (fl *fileLock) Lock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	// 락 파일 디렉토리 생성
	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	// 락 파일 열기 (없으면 생성, 기존 내용 삭제)
	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	// non-blocking 파일 락 시도
	for i := 0; i < 100; i++ { // 최대 10초 대기
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			fl.file = file
			fl.locked = true
			fl.writeLockInfo()
			return nil
		}

		// 락이 이미 존재하는지 확인
		if fl.isLockStale() {
			// 현재 파일 포인터 닫고 stale 락 제거 후 다시 열기
			file.Close()
			fl.removeStaleLock()
			file, err = os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			continue
		}

		// 잠시 대기 후 재시도
		time.Sleep(100 * time.Millisecond)
	}

	file.Close()
	return &LockError{
		Path:    fl.path,
		Message: "failed to acquire file lock after timeout",
	}
}

// Unlock은 파일 락을 해제합니다.
// 락 파일은 삭제하지 않습니다 - isLockStale()이 ModTime 기반으로 처리합니다.
// 파일을 삭제하면 다른 프로세스가 락을 획득한 직후 파일이 사라지는 크로스 프로세스 레이스가 발생합니다.
func (fl *fileLock) Unlock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.file == nil {
		return nil
	}

	// flock 해제
	err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
	if cerr := fl.file.Close(); cerr != nil && err == nil {
		err = cerr
	}
	fl.locked = false
	fl.file = nil

	return err
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

// tryLock은 non-blocking으로 락을 시도합니다.
func (fl *fileLock) tryLock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return err
	}

	fl.file = file
	fl.locked = true
	fl.writeLockInfo()
	return nil
}

// RLock은 읽기 락을 획득합니다.
func (fl *fileLock) RLock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if err == nil {
			fl.file = file
			fl.locked = true
			return nil
		}

		// stale 락 검사
		if fl.isLockStale() {
			file.Close()
			fl.removeStaleLock()
			file, err = os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return err
			}
			continue
		}

		time.Sleep(100 * time.Millisecond)
	}

	file.Close()
	return &LockError{
		Path:    fl.path,
		Message: "failed to acquire read lock after timeout",
	}
}

// RUnlock은 읽기 락을 해제합니다.
// 읽기 락은 여러 프로세스가 동시에 보유할 수 있으므로 lock 파일을 삭제하지 않습니다.
func (fl *fileLock) RUnlock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.file == nil {
		return nil
	}

	err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
	if cerr := fl.file.Close(); cerr != nil && err == nil {
		err = cerr
	}
	fl.locked = false
	fl.file = nil

	return err
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
