//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// hardenPrivateFile clears inherited ACL entries from the empty export staging
// directory. On macOS a directory ACL can grant read access that mode bits
// alone do not revoke, and the export file is created inside the directory only
// after the ACL has been cleared, so it inherits no grants for other accounts.
func hardenPrivateFile(path string) error {
	out, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clear export ACL: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensurePrivateFileSupported() error {
	return nil
}
