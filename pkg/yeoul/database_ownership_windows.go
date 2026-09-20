//go:build windows

package yeoul

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockDatabaseOwnershipFile takes a LockFileEx lock over the first byte of the
// ownership file. Windows releases the lock when the owning process terminates,
// so a killed migration never leaves an ownership file that later opens cannot
// reclaim. Acquisition is atomic, so a competing owner is detected by the lock
// call itself rather than by a stat call it can invalidate later.
func lockDatabaseOwnershipFile(file *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return errDatabaseOwnershipBusy
	default:
		return err
	}
}

func unlockDatabaseOwnershipFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
