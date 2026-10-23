//go:build linux || darwin

package yeoul

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRhizaAllowsOnlyOneLocalOwner(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "single-owner")
	first, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open first Rhiza owner: %v", err)
	}
	defer first.Close(ctx)
	if _, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true}); err == nil {
		t.Fatal("Rhiza accepted a second local process owner")
	}
}
