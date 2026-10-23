//go:build !linux && !darwin

package yeoul

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

func TestUnsupportedRhizaRefusesWithoutTouchingExistingOrNewPaths(t *testing.T) {
	if rhizastore.Supported() {
		t.Skip("Rhiza native adapter is supported on this platform")
	}
	ctx := context.Background()
	root := t.TempDir()
	newPath := filepath.Join(root, "new-rhiza")
	if _, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: newPath, CreateIfMissing: true}); !hasErrorCode(err, ErrNotSupported) {
		t.Fatalf("explicit new Rhiza error = %v, want not-supported", err)
	}
	for _, path := range []string{newPath, databaseOwnershipPath(newPath)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("unsupported Rhiza creation touched %q: %v", path, err)
		}
	}

	existingPath := filepath.Join(root, "existing-rhiza")
	if err := os.Mkdir(existingPath, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existingPath, rhizaDriverMarker)
	if err := os.WriteFile(marker, []byte(rhizaDriverMarkerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{DatabasePath: existingPath}); !hasErrorCode(err, ErrNotSupported) {
		t.Fatalf("existing Rhiza error = %v, want not-supported", err)
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(after) != string(before) {
		t.Fatalf("existing Rhiza marker changed: %q, %v", after, err)
	}
	if _, err := os.Lstat(databaseOwnershipPath(existingPath)); !os.IsNotExist(err) {
		t.Fatalf("existing Rhiza refusal created an ownership file: %v", err)
	}
}

func TestUnsupportedPlatformDefaultsNewDatabasesToLattice(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "new-default")
	eng, err := Open(ctx, Config{DatabasePath: path, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*engine).store.(*ownershipStore).stateStore.(*latticeStore); !ok {
		t.Fatalf("default store type = %T, want Lattice fallback", eng.(*engine).store)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, rhizaDriverMarker)); !os.IsNotExist(err) {
		t.Fatalf("Lattice fallback created a Rhiza marker: %v", err)
	}
}
