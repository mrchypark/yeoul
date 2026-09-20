//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

// hardenPrivateFile replaces the inherited ACL on the empty export staging
// directory with a grant for the current user only, so the export file created
// inside it inherits no read grants for other accounts.
func hardenPrivateFile(path string) error {
	account, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current user for export ACL: %w", err)
	}
	out, err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", account.Username+":F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("set export ACL: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensurePrivateFileSupported() error {
	return nil
}
