//go:build windows

package yeoul

import (
	"os"
	"path/filepath"
	"time"
)

// Lock은 파일 락을 획득합니다 (Windows: 파일 독점 열기로 구현).
func (fl *fileLock) Lock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		// Windows에서 파일 락: O_CREATE|O_RDWR로 열되, 다른 프로세스가 열지 못하도록 시도
		file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0644)
		if err == nil {
			fl.file = file
			fl.locked = true
			fl.writeLockInfo()
			return nil
		}

		if fl.isLockStale() {
			fl.removeStaleLock()
			continue
		}

		time.Sleep(100 * time.Millisecond)
	}

	return &LockError{
		Path:    fl.path,
		Message: "failed to acquire file lock after timeout",
	}
}

// Unlock은 파일 락을 해제합니다.
func (fl *fileLock) Unlock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.file == nil {
		return nil
	}

	err := fl.file.Close()
	fl.locked = false
	fl.file = nil

	return err
}

// tryLock은 non-blocking으로 락을 시도합니다.
func (fl *fileLock) tryLock() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	dir := filepath.Dir(fl.path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0644)
	if err != nil {
		return err
	}

	fl.file = file
	fl.locked = true
	fl.writeLockInfo()
	return nil
}

// RLock은 읽기 락을 획득합니다 (Windows: Lock()과 동일하게 구현).
func (fl *fileLock) RLock() error {
	return fl.Lock()
}

// RUnlock은 읽기 락을 해제합니다.
func (fl *fileLock) RUnlock() error {
	return fl.Unlock()
}
