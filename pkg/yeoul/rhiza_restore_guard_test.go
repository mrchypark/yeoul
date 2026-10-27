//go:build linux || darwin

package yeoul

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRhizaRestoreAccessDeniedLeavesPendingGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Access Denied</Message><RequestId>test</RequestId></Error>`))
	}))
	defer server.Close()

	root := t.TempDir()
	dbPath := filepath.Join(root, "remote-lost")
	backup := &RhizaBackupConfig{
		Provider:  "s3",
		Endpoint:  server.URL,
		Bucket:    "yeoul-test-bucket",
		Prefix:    "nonblank/recovery-prefix",
		Region:    "us-east-1",
		Insecure:  true,
		AccessKey: "fixed-test-access-key",
		SecretKey: "fixed-test-secret-key",
	}

	_, err := Open(context.Background(), Config{
		Driver:           StorageDriverRhiza,
		DatabasePath:     dbPath,
		RhizaBackup:      backup,
		RestoreIfMissing: true,
	})
	if err == nil {
		t.Fatal("restore unexpectedly succeeded against AccessDenied endpoint")
	}

	markerPath := rhizaRestorePendingMarkerPath(dbPath)
	if info, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("restore-pending marker missing after failed restore: %v", statErr)
	} else if !info.Mode().IsRegular() {
		t.Fatalf("restore-pending marker is not a regular file: mode %v", info.Mode())
	}

	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "default driver", cfg: Config{DatabasePath: dbPath}},
		{name: "create if missing", cfg: Config{DatabasePath: dbPath, CreateIfMissing: true}},
		{name: "explicit lattice", cfg: Config{Driver: StorageDriverLattice, DatabasePath: dbPath}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, openErr := Open(context.Background(), tc.cfg); openErr == nil {
				t.Fatal("ordinary open accepted database with pending Rhiza restore")
			}
			if _, statErr := os.Stat(markerPath); statErr != nil {
				t.Fatalf("restore-pending marker changed or disappeared: %v", statErr)
			}
		})
	}
}

func TestRhizaBackupConfigRejectsInvalidConfigurations(t *testing.T) {
	validBackup := func() *RhizaBackupConfig {
		return &RhizaBackupConfig{Provider: "s3", Bucket: "bucket", Prefix: "prefix"}
	}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{
			name: "unsupported provider",
			cfg:  Config{Driver: StorageDriverRhiza, RhizaBackup: &RhizaBackupConfig{Provider: "unknown", Bucket: "bucket", Prefix: "prefix"}},
		},
		{
			name: "blank prefix",
			cfg:  Config{Driver: StorageDriverRhiza, RhizaBackup: &RhizaBackupConfig{Provider: "s3", Bucket: "bucket", Prefix: "  "}},
		},
		{
			name: "create and restore",
			cfg:  Config{Driver: StorageDriverRhiza, CreateIfMissing: true, RestoreIfMissing: true, RhizaBackup: validBackup()},
		},
		{
			name: "in-memory backup",
			cfg:  Config{Driver: StorageDriverRhiza, InMemory: true, RhizaBackup: validBackup()},
		},
		{
			name: "read-only backup",
			cfg:  Config{Driver: StorageDriverRhiza, ReadOnly: true, RhizaBackup: validBackup()},
		},
		{
			name: "read-only restore",
			cfg:  Config{Driver: StorageDriverRhiza, ReadOnly: true, RestoreIfMissing: true, RhizaBackup: validBackup()},
		},
		{
			name: "azure key and connection string",
			cfg: Config{Driver: StorageDriverRhiza, RhizaBackup: &RhizaBackupConfig{
				Provider: "azure", Bucket: "bucket", Prefix: "prefix",
				AzureStorageAccountKey: "key", AzureConnectionString: "connection",
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRhizaBackupConfig(tc.cfg); err == nil {
				t.Fatal("invalid Rhiza backup configuration was accepted")
			}
		})
	}
}
