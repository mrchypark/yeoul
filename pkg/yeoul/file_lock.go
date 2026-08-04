package yeoul

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// fileLock은 프로세스 간 파일 락을 관리합니다.
type fileLock struct {
	path   string
	file   *os.File
	locked bool
}

// newFileLock은 새로운 파일 락을 생성합니다.
func newFileLock(path string) *fileLock {
	return &fileLock{
		path: filepath.Dir(path) + "/.yeoul.lock",
	}
}

// Lock은 파일 락을 획득합니다.
func (fl *fileLock) Lock() error {
	// 락 파일 디렉토리 생성
	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	// 락 파일 열기 (없으면 생성)
	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	//非blocking 파일 락 시도
	for i := 0; i < 100; i++ { // 최대 10초 대기
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			fl.file = file
			fl.locked = true
			// 락 정보 기록
			fl.writeLockInfo()
			return nil
		}

		// 락이 이미 존재하는지 확인
		if fl.isLockStale() {
			// 오래된 락 제거
			fl.removeStaleLock()
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
func (fl *fileLock) Unlock() error {
	if fl.file == nil {
		return nil
	}

	// flock 해제 (먼저 해제해야 함)
	err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
	fl.file.Close()
	fl.locked = false
	fl.file = nil

	// 락 정보 파일 제거
	fl.removeLockInfo()

	return err
}

// writeLockInfo는 락 정보를 파일에 기록합니다.
func (fl *fileLock) writeLockInfo() {
	if fl.file == nil {
		return
	}

	// 파일 끝에 락 정보 기록 (Truncate/Seek 불필요)
	data := []byte("pid: " + strconv.Itoa(os.Getpid()) + "\n")
	fl.file.Write(data)
}

// removeLockInfo는 락 정보 파일을 제거합니다.
func (fl *fileLock) removeLockInfo() {
	os.Remove(fl.path)
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
	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0644)
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
		time.Sleep(100 * time.Millisecond)
	}

	file.Close()
	return &LockError{
		Path:    fl.path,
		Message: "failed to acquire read lock after timeout",
	}
}

// RUnlock은 읽기 락을 해제합니다.
func (fl *fileLock) RUnlock() error {
	return fl.Unlock()
}

// IsLocked는 락이 획득되었는지 확인합니다.
func (fl *fileLock) IsLocked() bool {
	return fl.locked
}

// Path는 락 파일 경로를 반환합니다.
func (fl *fileLock) Path() string {
	return fl.path
}
