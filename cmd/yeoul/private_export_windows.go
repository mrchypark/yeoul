//go:build windows

package main

import "fmt"

// ensurePrivateFileSupported refuses exports on Windows: the platform can grant
// read access through inherited ACL entries, and no available API creates a
// file with private effective access atomically, so an export could be exposed
// between file creation and hardening.
func ensurePrivateFileSupported() error {
	return fmt.Errorf("admin export is not supported on Windows because a private export file cannot be created atomically; run the export on Linux")
}
