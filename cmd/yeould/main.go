package main

import (
	"fmt"
	"os"
)

// yeould is a placeholder for a future local service adapter. It reports that
// no daemon was started and exits with a failure status so scripts cannot
// mistake the stub for a running service.
func main() {
	fmt.Fprintln(os.Stderr, "yeould is not implemented in this release: the local service adapter is deferred and no daemon was started")
	os.Exit(2)
}
