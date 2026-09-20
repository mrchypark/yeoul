//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package yeoul

import (
	"fmt"
	"os"
	"runtime"
)

// lockDatabaseOwnershipFile refuses to open a database on a platform without an
// implementation of the database ownership lock. Reporting the gap is safer
// than silently opening without ownership, which would reintroduce the
// concurrent-migration defects the lock exists to prevent.
func lockDatabaseOwnershipFile(*os.File, bool) error {
	return fmt.Errorf("database ownership locking is not implemented on %s", runtime.GOOS)
}

func unlockDatabaseOwnershipFile(*os.File) error {
	return nil
}
