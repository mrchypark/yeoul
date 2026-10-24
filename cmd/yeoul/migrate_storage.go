package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type storageMigrationResult struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
	Migrated   bool   `json:"migrated"`
}

func (c cli) runMigrateStorage(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul migrate-storage --source LATTICE_PATH --target NEW_RHIZA_PATH [--json]
`)
	fs := newFlagSet("migrate-storage")
	var sourcePath, targetPath string
	var jsonOut bool
	fs.StringVar(&sourcePath, "source", "", "existing Lattice database path")
	fs.StringVar(&targetPath, "target", "", "new Rhiza database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || sourcePath == "" || targetPath == "" {
		return &usageError{message: usage}
	}
	if err := yeoul.MigrateLatticeToRhiza(ctx, sourcePath, targetPath); err != nil {
		return err
	}
	result := storageMigrationResult{SourcePath: sourcePath, TargetPath: targetPath, Migrated: true}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "migrated %s to %s\n", sourcePath, targetPath)
	return err
}
