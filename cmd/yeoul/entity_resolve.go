package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

// runEntityResolve looks an entity up by its identity tuple instead of by ID.
// The derived entity ID is a hash of the exact tuple, so a caller that only
// knows the identity cannot recompute the canonical ID when the stored
// namespace or type drifted (issue #139).
func (c cli) runEntityResolve(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul entity resolve --db PATH --type TYPE (--name NAME | --stable-key KEY) [--namespace NS] [--space ID] [--json]
`)

	fs := newFlagSet("entity resolve")
	var dbPath string
	var entityType string
	var name string
	var stableKey string
	var namespace string
	var space string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&entityType, "type", "", "entity type")
	fs.StringVar(&name, "name", "", "entity canonical name")
	fs.StringVar(&stableKey, "stable-key", "", "entity stable identity key")
	fs.StringVar(&namespace, "namespace", "", "entity namespace")
	fs.StringVar(&space, "space", "", "record space ID")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(entityType) == "" {
		return &usageError{message: usage}
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(stableKey) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.ResolveEntity(ctx, yeoul.EntityResolveRequest{
		SpaceID:       space,
		Namespace:     namespace,
		Type:          entityType,
		CanonicalName: name,
		StableKey:     stableKey,
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		// A missing entity is the engine own ErrEntityNotFound, which the CLI
		// maps to exit code 3.
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, resp)
	}

	drifted := make(map[string]bool, len(resp.Drifted))
	for _, entity := range resp.Drifted {
		drifted[entity.ID] = true
	}
	for _, entity := range resp.Matches {
		if _, err := fmt.Fprintf(c.stdout, "match=%s namespace=%s type=%s name=%s drifted=%t\n", entity.ID, entity.Namespace, entity.Type, entity.CanonicalName, drifted[entity.ID]); err != nil {
			return err
		}
	}
	return nil
}
