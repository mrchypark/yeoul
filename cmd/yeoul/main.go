package main

import (
	"context"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		printCommandError(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}
