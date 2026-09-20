package yeoul

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// databaseOwnershipSuffix names the sibling file that carries the OS lock
// which serializes database ownership. It reuses the migration lock name so one
// database keeps a single ownership namespace.
const databaseOwnershipSuffix = ".yeoul-migration.lock"

// errDatabaseOwnershipBusy reports that another process holds the database
// ownership lock right now. Callers translate it into "database migration is in
// progress" or into a retry, depending on the lock they asked for.
var errDatabaseOwnershipBusy = errors.New("database ownership is held by another process")

// databaseOwnershipLock is an OS-managed advisory lock on one database's
// ownership file.
//
// The lock lives in the kernel and is tied to the open file description, so the
// operating system drops it when the owning process exits. That is what makes a
// crash recoverable without PID bookkeeping: a killed owner can never leave a
// lock that later opens must reclaim, and no caller ever removes the lock file
// on behalf of another owner.
type databaseOwnershipLock struct {
	file *os.File
}

func databaseOwnershipPath(databasePath string) string {
	return databasePath + databaseOwnershipSuffix
}

// acquireDatabaseOwnership takes the advisory lock that guards one database
// path. exclusive is the migration lock; every ordinary open holds the shared
// lock for the store's lifetime so a migration cannot snapshot a database that
// a live store is still changing and cannot install a replacement underneath
// it.
//
// Acquisition is atomic, so ownership is never inferred from a stat call that a
// competing process can invalidate afterwards.
func acquireDatabaseOwnership(databasePath string, exclusive bool) (*databaseOwnershipLock, error) {
	file, err := openDatabaseOwnershipFile(databasePath)
	if err != nil {
		return nil, err
	}
	if err := lockDatabaseOwnershipFile(file, exclusive); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &databaseOwnershipLock{file: file}, nil
}

// ensureDatabaseOwnershipDirectory creates the directory that will hold a new
// database before ownership is acquired for it, so an explicit creation of a
// database in a path whose parent does not exist yet still gets the ownership
// lock instead of failing with a missing-directory error. The database itself
// is still created only after the lock is held.
func ensureDatabaseOwnershipDirectory(databasePath string) error {
	parent := filepath.Dir(databasePath)
	if parent == "" || parent == "." {
		return nil
	}
	if _, err := os.Stat(parent); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create database directory %q: %w", parent, err)
	}
	return nil
}

// openDatabaseOwnershipFile opens the ownership file, creating it when the
// directory allows that. A database in a directory that forbids creating the
// file can still be locked through an existing ownership file opened read-only;
// when there is none the permission error is reported so the caller can decide
// whether a read-only open may proceed without ownership.
func openDatabaseOwnershipFile(databasePath string) (*os.File, error) {
	lockPath := databaseOwnershipPath(databasePath)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return nil, fmt.Errorf("open database ownership file %q: %w", lockPath, err)
	}
	file, readErr := os.OpenFile(lockPath, os.O_RDONLY, 0)
	if readErr != nil {
		return nil, errors.Join(
			fmt.Errorf("open database ownership file %q: %w", lockPath, err),
			fmt.Errorf("open existing database ownership file %q read-only: %w", lockPath, readErr),
		)
	}
	return file, nil
}

// Release unlocks and closes the ownership file. The lock file itself stays in
// place: it is an empty marker for the lock, not the lock.
func (l *databaseOwnershipLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockDatabaseOwnershipFile(file), file.Close())
}
