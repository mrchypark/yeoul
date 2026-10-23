//go:build windows

package yeoul

// Windows does not expose directory fsync through the Go standard library.
func syncStorageDirectory(string) error { return nil }

func storageMigrationDurable() bool { return false }
