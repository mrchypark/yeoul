//go:build !darwin && !windows

package main

// hardenPrivateFile is a no-op where POSIX mode bits determine effective access
// for other local accounts.
func hardenPrivateFile(string) error {
	return nil
}

func ensurePrivateFileSupported() error {
	return nil
}
