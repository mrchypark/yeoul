//go:build !windows

package yeoul

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock은 파일 락을 획득합니다.
func (fl *fileLock) Lock() error {
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

	for i := 0; i < 100; i++ {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			fl.file = file
			fl.locked = true
			fl.writeLockInfo()
			return nil
		}

		if fl.isLockStale() {
			file.Close()
			fl.removeStaleLock()
			file, err = os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
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

	err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
	if cerr := fl.file.Close(); cerr != nil && err == nil {
		err = cerr
	}
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
