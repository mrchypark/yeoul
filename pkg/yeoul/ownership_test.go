package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenHoldsOwnershipForStoreLifetime(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "held.ltdb")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := acquireDatabaseOwnership(dbPath, true); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected exclusive ownership to be refused while a store is open, got %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("expected ownership to be free after close: %v", err)
	}
	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
}

func TestConcurrentReadersShareOwnership(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.ltdb")
	created, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	if err := created.Close(ctx); err != nil {
		t.Fatalf("close created database: %v", err)
	}
	first, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open first reader: %v", err)
	}
	second, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		_ = first.Close(ctx)
		t.Fatalf("open second reader: %v", err)
	}
	if err := errors.Join(second.Close(ctx), first.Close(ctx)); err != nil {
		t.Fatalf("close readers: %v", err)
	}
}
