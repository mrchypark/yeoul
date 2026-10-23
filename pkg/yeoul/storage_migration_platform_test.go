package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

func TestStorageMigrationRefusesPlatformsWithoutDirectoryDurabilityBeforeWrites(t *testing.T) {
	if storageMigrationDurable() && rhizastore.Supported() {
		t.Skip("platform supports Rhiza and durable directory synchronization")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	err := MigrateLatticeToRhiza(context.Background(), source, target)
	if !hasErrorCode(err, ErrNotSupported) {
		t.Fatalf("migration error = %v, want not-supported", err)
	}
	for _, path := range []string{source, target, databaseMigrationMarkerPath(target), databaseOwnershipPath(target)} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("migration touched %q: %v", path, err)
		}
	}
}
