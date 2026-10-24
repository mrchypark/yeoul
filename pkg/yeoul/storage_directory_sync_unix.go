//go:build !windows

package yeoul

import (
	"errors"
	"os"
)

func syncStorageDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func storageMigrationDurable() bool { return true }
