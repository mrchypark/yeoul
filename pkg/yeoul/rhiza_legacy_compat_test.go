//go:build linux || darwin

package yeoul

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const legacyFixtureSentinel = "legacy v0.20 Rhiza archive sentinel"

func TestRhiza020ArchiveCompatibility(t *testing.T) {
	if os.Getenv("YEOUL_RECOVERY_LEGACY") != "1" {
		t.Skip("set YEOUL_RECOVERY_LEGACY=1 through the Floci recovery harness")
	}
	provider := strings.ToLower(os.Getenv("YEOUL_RECOVERY_PROVIDER"))
	backup := rhizaBackupConfigFromEnv(t, provider)
	backup.Bucket = "yeoul-legacy-" + strings.ToLower(rand.Text())
	assertNewRecoveryBucket(t, backup)
	bootstrapRecoveryBucket(t, backup)
	emptyArchive, err := snapshotArchiveObjects(context.Background(), backup)
	if err != nil {
		t.Fatalf("verify new legacy bucket is empty: %v", err)
	}
	if len(emptyArchive) != 0 {
		t.Fatalf("new legacy bucket unexpectedly contains %d objects", len(emptyArchive))
	}
	backup.Prefix = recoveryPrefix("legacy-v020", recoveryUniqueSuffix())

	root := t.TempDir()
	ownerToken := recoveryUniqueSuffix()
	if err := os.WriteFile(filepath.Join(root, ".yeoul-legacy-fixture-owner"), []byte(ownerToken), 0o600); err != nil {
		t.Fatal(err)
	}
	oldDataDir := filepath.Join(root, "yeoul-v012-rhiza-v020")
	newDataDir := filepath.Join(root, "yeoul-v021-restore")
	if _, err := os.Lstat(newDataDir); !os.IsNotExist(err) {
		t.Fatalf("v0.21 restore target must be absent before restore: %v", err)
	}
	if _, err := os.Lstat(oldDataDir); !os.IsNotExist(err) {
		t.Fatalf("legacy source path must be absent before helper invocation: %v", err)
	}
	if err := runLegacyFixtureGenerator(t, oldDataDir, root, ownerToken, backup); err != nil {
		t.Fatalf("published v0.12.0 fixture generator failed: %v", err)
	}
	oldDBBefore, err := hashDirectory(oldDataDir)
	if err != nil {
		t.Fatalf("hash closed legacy database: %v", err)
	}
	oldDBLayout, err := directoryManifest(oldDataDir)
	if err != nil {
		t.Fatalf("read closed legacy database layout: %v", err)
	}
	archiveBefore, err := snapshotArchiveObjects(context.Background(), backup)
	if err != nil {
		t.Fatalf("read legacy archive with provider SDK: %v", err)
	}
	if len(archiveBefore) == 0 {
		t.Fatal("v0.20 fixture generator produced no archive objects")
	}
	t.Logf("legacy fixture before probe: provider=%s data_dir_sha256=%s data_dir_layout=%s archive_objects=%d archive_sha256=%s archive_layout=%s", provider, oldDBBefore, oldDBLayout, len(archiveBefore), hashObjects(archiveBefore), archiveManifest(archiveBefore))

	currentBackup := *backup
	ctx := context.Background()
	current, openErr := Open(ctx, Config{
		Driver: StorageDriverRhiza, DatabasePath: newDataDir,
		RhizaBackup: &currentBackup, RestoreIfMissing: true,
	})
	if openErr != nil {
		if nativeBindingRefusal(openErr) {
			t.Logf("BINDING_REFUSAL_NO_MUTATION: exact native binding error under YEOUL_STORAGE_FAILED; this does not establish archive-version validation: %T: %v; %s", openErr, openErr, recoveryErrorCauses(openErr, backup))
		} else {
			t.Logf("INCONCLUSIVE/BLOCKED Open error (not the observed native binding refusal): %T: %v; %s", openErr, openErr, recoveryErrorCauses(openErr, backup))
		}
	} else {
		t.Log("COMPATIBLE_ACCEPTANCE: v0.21 accepted the real v0.20 archive; checking public Yeoul semantic contract")
	}
	newDataAfterOpen, localErr := hashDirectory(newDataDir)
	if localErr != nil {
		t.Fatalf("hash v0.21 restore directory after Open: %v", localErr)
	}
	t.Logf("v0.21 restore directory immediately after Open: sha256=%s", newDataAfterOpen)
	archiveAfterOpen, archiveErr := snapshotArchiveObjects(ctx, backup)
	if archiveErr != nil {
		t.Fatalf("read archive immediately after v0.21 Open: %v", archiveErr)
	}
	oldDBAfterOpen, oldErr := hashDirectory(oldDataDir)
	if oldErr != nil {
		t.Fatalf("hash legacy database immediately after v0.21 Open: %v", oldErr)
	}
	t.Logf("v0.21 immediate post-open hashes: old_data_dir=%s target_data_dir=%s archive_objects=%d archive_sha256=%s archive_layout=%s", oldDBAfterOpen, newDataAfterOpen, len(archiveAfterOpen), hashObjects(archiveAfterOpen), archiveManifest(archiveAfterOpen))
	if openErr != nil {
		pending, markerErr := rhizaRestorePending(newDataDir)
		if markerErr != nil || !pending {
			t.Errorf("failed v0.20 restore did not retain its fail-closed pending marker: pending=%t err=%v", pending, markerErr)
		} else {
			for _, tc := range []struct {
				name string
				cfg  Config
			}{
				{name: "default driver", cfg: Config{DatabasePath: newDataDir}},
				{name: "create if missing", cfg: Config{DatabasePath: newDataDir, CreateIfMissing: true}},
				{name: "explicit lattice", cfg: Config{Driver: StorageDriverLattice, DatabasePath: newDataDir}},
				{name: "explicit Rhiza", cfg: Config{Driver: StorageDriverRhiza, DatabasePath: newDataDir}},
			} {
				t.Run("pending guard/"+tc.name, func(t *testing.T) {
					if _, retryErr := Open(ctx, tc.cfg); retryErr == nil {
						t.Fatal("ordinary open accepted database with pending Rhiza restore")
					}
					if stillPending, statErr := rhizaRestorePending(newDataDir); statErr != nil || !stillPending {
						t.Fatalf("ordinary open changed or removed pending marker: pending=%t err=%v", stillPending, statErr)
					}
				})
			}
		}
	}

	if openErr == nil {
		state, snapshotErr := Snapshot(ctx, current)
		if snapshotErr != nil {
			_ = current.Close(ctx)
			t.Fatalf("snapshot accepted v0.20 restore: %v", snapshotErr)
		}
		if episode, ok := state.Episodes["ep:legacy-v020"]; !ok || episode.Content != legacyFixtureSentinel {
			_ = current.Close(ctx)
			t.Fatalf("accepted v0.20 archive lost its episode sentinel: %#v", episode)
		}
		if fact, ok := state.Facts["fact:legacy-v020"]; !ok || fact.ValueText != "Yeoul v0.12.0 with Rhiza v0.20.0" {
			_ = current.Close(ctx)
			t.Fatalf("accepted v0.20 archive lost its fact sentinel: %#v", fact)
		}
		if err := current.Close(ctx); err != nil {
			t.Fatalf("close v0.21 restored database: %v", err)
		}
	}

	archiveAfter, err := snapshotArchiveObjects(ctx, backup)
	if err != nil {
		t.Fatalf("read archive after v0.21 probe: %v", err)
	}
	oldDBAfter, err := hashDirectory(oldDataDir)
	if err != nil {
		t.Fatalf("hash legacy database after v0.21 probe: %v", err)
	}
	newDataAfter, err := hashDirectory(newDataDir)
	if err != nil {
		t.Fatalf("hash v0.21 database after probe: %v", err)
	}
	t.Logf("v0.21 final hashes: old_data_dir=%s target_data_dir=%s archive_objects=%d archive_sha256=%s archive_layout=%s", oldDBAfter, newDataAfter, len(archiveAfter), hashObjects(archiveAfter), archiveManifest(archiveAfter))
	if oldDBAfterOpen != oldDBBefore || oldDBAfter != oldDBBefore {
		t.Errorf("v0.21 probe modified closed legacy data directory: before=%s after_open=%s after=%s", oldDBBefore, oldDBAfterOpen, oldDBAfter)
	}
	if diff := archiveDifference(archiveBefore, archiveAfterOpen); diff != "" {
		t.Errorf("v0.21 Open mutated legacy archive: %s", diff)
	}
	if diff := archiveDifference(archiveBefore, archiveAfter); diff != "" {
		t.Errorf("complete v0.21 probe mutated legacy archive: %s", diff)
	}
	if openErr != nil {
		if !nativeBindingRefusal(openErr) {
			t.Errorf("INCONCLUSIVE/BLOCKED: Open did not return the exact observed native binding cause under YEOUL_STORAGE_FAILED; observed %s", recoveryErrorCauses(openErr, backup))
		}
	}
}

func nativeBindingRefusal(err error) bool {
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != ErrStorageFailed {
		return false
	}
	return hasExactErrorCause(err, "invalid Local archive binding")
}

func hasExactErrorCause(err error, message string) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if hasExactErrorCause(cause, message) {
				return true
			}
		}
		return false
	}
	if cause := errors.Unwrap(err); cause != nil {
		return hasExactErrorCause(cause, message)
	}
	return err.Error() == message
}

func TestNativeBindingRefusalRequiresExactCauseUnderStorageFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "generic storage", err: &Error{Code: ErrStorageFailed, Message: "open Rhiza database"}},
		{name: "binding text without public storage error", err: errors.New("invalid Local archive binding")},
		{name: "unrelated joined storage cause", err: errors.Join(&Error{Code: ErrStorageFailed, Message: "open Rhiza database"}, errors.New("connection refused"))},
		{name: "binding cause under joined storage error", err: errors.Join(&Error{Code: ErrStorageFailed, Message: "open Rhiza database"}, errors.New("invalid Local archive binding")), want: true},
		{name: "binding cause under wrong public error", err: errors.Join(&Error{Code: ErrNotSupported, Message: "unsupported archive"}, errors.New("invalid Local archive binding"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeBindingRefusal(tc.err); got != tc.want {
				t.Fatalf("nativeBindingRefusal() = %t, want %t", got, tc.want)
			}
		})
	}
}

func runLegacyFixtureGenerator(t *testing.T, dbPath, ownerRoot, ownerToken string, backup *RhizaBackupConfig) error {
	t.Helper()
	helper := os.Getenv("YEOUL_LEGACY_FIXTURE_HELPER")
	if helper == "" {
		return fmt.Errorf("YEOUL_LEGACY_FIXTURE_HELPER is required")
	}
	cmd := exec.Command(helper)
	cmd.Env = legacyHelperEnv(dbPath, ownerRoot, ownerToken, backup)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exit=%v output=%s", err, output)
	}
	t.Logf("legacy generator: %s", strings.TrimSpace(string(output)))
	return nil
}

func legacyHelperEnv(dbPath, ownerRoot, ownerToken string, backup *RhizaBackupConfig) []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "AWS") || strings.HasPrefix(upper, "GOOGLE") || strings.HasPrefix(upper, "AZURE") || strings.HasPrefix(upper, "YEOUL_") || upper == "STORAGE_EMULATOR_HOST" {
			continue
		}
		env = append(env, item)
	}
	endpoint := os.Getenv("YEOUL_RECOVERY_" + strings.ToUpper(backup.Provider) + "_ENDPOINT")
	values := []string{
		"GOWORK=off", "YEOUL_LEGACY_DB_PATH=" + dbPath,
		"YEOUL_LEGACY_ROOT=" + ownerRoot, "YEOUL_LEGACY_OWNER_TOKEN=" + ownerToken,
		"YEOUL_LEGACY_PROVIDER=" + backup.Provider, "YEOUL_LEGACY_ENDPOINT=" + endpoint,
		"YEOUL_LEGACY_BUCKET=" + backup.Bucket, "YEOUL_LEGACY_PREFIX=" + backup.Prefix,
	}
	if backup.Provider == "gcs" {
		values = append(values, "STORAGE_EMULATOR_HOST="+endpoint, "GOOGLE_CLOUD_PROJECT=yeoul-local-test", "GOOGLE_APPLICATION_CREDENTIALS=")
	}
	return append(env, values...)
}

func snapshotArchiveObjects(ctx context.Context, backup *RhizaBackupConfig) (map[string][]byte, error) {
	objects := make(map[string][]byte)
	switch backup.Provider {
	case "s3":
		client, err := minio.New(backup.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(backup.AccessKey, backup.SecretKey, ""),
			Secure: false, Region: backup.Region, BucketLookup: minio.BucketLookupPath,
		})
		if err != nil {
			return nil, err
		}
		for object := range client.ListObjects(ctx, backup.Bucket, minio.ListObjectsOptions{Recursive: true}) {
			if object.Err != nil {
				return nil, object.Err
			}
			reader, err := client.GetObject(ctx, backup.Bucket, object.Key, minio.GetObjectOptions{})
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				return nil, err
			}
			objects[object.Key] = data
		}
	case "gcs":
		client, err := storage.NewClient(ctx, option.WithEndpoint(os.Getenv("STORAGE_EMULATOR_HOST")), option.WithoutAuthentication())
		if err != nil {
			return nil, err
		}
		defer client.Close()
		it := client.Bucket(backup.Bucket).Objects(ctx, nil)
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				return nil, err
			}
			reader, err := client.Bucket(backup.Bucket).Object(attrs.Name).NewReader(ctx)
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				return nil, err
			}
			objects[attrs.Name] = data
		}
	case "azure":
		client, err := azblob.NewClientFromConnectionString(backup.AzureConnectionString, nil)
		if err != nil {
			return nil, err
		}
		container := client.ServiceClient().NewContainerClient(backup.Bucket)
		pager := container.NewListBlobsFlatPager(nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, item := range page.Segment.BlobItems {
				name := *item.Name
				response, err := client.DownloadStream(ctx, backup.Bucket, name, nil)
				if err != nil {
					return nil, err
				}
				reader := response.Body
				data, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				if err := errors.Join(readErr, closeErr); err != nil {
					return nil, err
				}
				objects[name] = data
			}
		}
	default:
		return nil, fmt.Errorf("unsupported provider %q", backup.Provider)
	}
	return objects, nil
}

func assertNewRecoveryBucket(t *testing.T, backup *RhizaBackupConfig) {
	t.Helper()
	ctx := context.Background()
	switch backup.Provider {
	case "s3":
		client, err := minio.New(backup.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(backup.AccessKey, backup.SecretKey, ""),
			Secure: false, Region: backup.Region, BucketLookup: minio.BucketLookupPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		exists, err := client.BucketExists(ctx, backup.Bucket)
		if err != nil || exists {
			t.Fatalf("random legacy bucket already exists or cannot be checked: exists=%t err=%v", exists, err)
		}
	case "gcs":
		client, err := storage.NewClient(ctx, option.WithEndpoint(os.Getenv("STORAGE_EMULATOR_HOST")), option.WithoutAuthentication())
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		_, err = client.Bucket(backup.Bucket).Attrs(ctx)
		if err == nil || !errors.Is(err, storage.ErrBucketNotExist) {
			t.Fatalf("random legacy bucket already exists or absence is unverified: %v", err)
		}
	case "azure":
		client, err := azblob.NewClientFromConnectionString(backup.AzureConnectionString, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ServiceClient().NewContainerClient(backup.Bucket).GetProperties(ctx, nil)
		var responseErr *azcore.ResponseError
		if err == nil || !errors.As(err, &responseErr) || responseErr.StatusCode != 404 {
			t.Fatalf("random legacy container already exists or absence is unverified: %v", err)
		}
	default:
		t.Fatalf("unsupported provider %q", backup.Provider)
	}
}

func recoveryUniqueSuffix() string { return strings.ToLower(rand.Text()) }

func hashDirectory(root string) (string, error) {
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return "<absent>", nil
	} else if err != nil {
		return "", err
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected non-regular fixture path %s", path)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func directoryManifest(root string) (string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), len(data), fixtureSHA256(data)))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	return strings.Join(entries, ","), nil
}

func hashObjects(objects map[string][]byte) string {
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		data := objects[key]
		contentHash := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%d\x00%x\n", key, len(data), contentHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func archiveManifest(objects map[string][]byte) string {
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		data := objects[key]
		entries = append(entries, fmt.Sprintf("%s:%d:%s", key, len(data), fixtureSHA256(data)))
	}
	return strings.Join(entries, ",")
}

func fixtureSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func archiveDifference(before, after map[string][]byte) string {
	if len(before) != len(after) {
		return fmt.Sprintf("object count before=%d after=%d", len(before), len(after))
	}
	for key, beforeBytes := range before {
		afterBytes, ok := after[key]
		if !ok || !bytes.Equal(beforeBytes, afterBytes) {
			return fmt.Sprintf("object %q before_sha256=%x after_sha256=%x", key, sha256.Sum256(beforeBytes), sha256.Sum256(afterBytes))
		}
	}
	return ""
}
