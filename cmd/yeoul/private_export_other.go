//go:build !darwin && !windows

package main

// ensurePrivateFileSupported allows exports where POSIX mode bits determine
// effective access for other local accounts.
func ensurePrivateFileSupported() error {
	return nil
}
