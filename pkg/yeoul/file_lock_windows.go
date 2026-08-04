//go:build windows

package yeoul

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock = 0x00000002
	lockfileFailImmediately = 0x00000001
)

func lockFile(f *os.File, exclusive bool) error {
	var flags uint32 = lockfileFailImmediately
	if exclusive {
		flags |= lockfileExclusiveLock
	}

	var overlapped syscall.Overlapped
	ret, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(flags),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if ret == 0 {
		if err.(syscall.Errno) == syscall.ERROR_LOCK_VIOLATION {
			return errLockBusy
		}
		return err
	}
	return nil
}

func unlockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	ret, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

var errLockBusy = &LockError{Message: "lock busy"}

// Lock은 파일 락을 획득합니다 (Windows: LockFileEx 사용).
func (fl *fileLock) Lock() error {
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
		if err := lockFile(file, true); err == nil {
			fl.file = file
			fl.locked = true
			fl.writeLockInfo()
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

	err := unlockFile(fl.file)
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

	file, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	if err := lockFile(file, true); err != nil {
		file.Close()
		return err
	}

	fl.file = file
	fl.locked = true
	fl.writeLockInfo()
	return nil
}

// RLock은 읽기 락을 획득합니다 (Windows: 공유 락 사용).
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
		if err := lockFile(file, false); err == nil {
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
	return fl.Unlock()
}
