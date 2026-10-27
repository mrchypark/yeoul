package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFreshTargetRequiresOwnedAbsentPathAndPreservesNeighbors(t *testing.T) {
	parent := t.TempDir()
	neighbor := filepath.Join(parent, "neighbor")
	if err := os.Mkdir(neighbor, 0o700); err != nil {
		t.Fatal(err)
	}
	neighborFile := filepath.Join(neighbor, "keep")
	if err := os.WriteFile(neighborFile, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(parent, "owned")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	token := "owner-token"
	marker := filepath.Join(root, ".yeoul-legacy-fixture-owner")
	if err := os.WriteFile(marker, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(filepath.Join(root, "fresh-db"), root, token); err != nil {
		t.Fatalf("valid fresh target rejected: %v", err)
	}

	existing := filepath.Join(root, "existing-db")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(existing, "keep")
	if err := os.WriteFile(sentinel, []byte("preserve existing database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(existing, root, token); err == nil {
		t.Fatal("accepted an existing database path")
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "preserve existing database" {
		t.Fatalf("existing database was changed: value=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(neighborFile); err != nil || string(got) != "untouched" {
		t.Fatalf("neighbor path was changed: value=%q err=%v", got, err)
	}

	unmarked := filepath.Join(parent, "unmarked")
	if err := os.Mkdir(unmarked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(filepath.Join(unmarked, "fresh-db"), unmarked, token); err == nil {
		t.Fatal("accepted an unmarked fixture root")
	}
}
