//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package yeoul

import (
	"errors"
	"os"
	"syscall"
)

// lockDatabaseOwnershipFile takes an advisory flock(2) lock. The lock belongs to
// the open file description and the kernel drops it when the last descriptor
// closes, which includes process termination, so a crashed owner never leaves an
// unreclaimable lock. flock is also atomic, so a competing owner is detected by
// the acquisition itself rather than by a stat call it can invalidate later.
func lockDatabaseOwnershipFile(file *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return errDatabaseOwnershipBusy
		default:
			return err
		}
	}
}

func unlockDatabaseOwnershipFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if errors.Is(err, syscall.EBADF) {
		return nil
	}
	return err
}
