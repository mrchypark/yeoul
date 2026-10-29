//go:build linux || darwin

package yeoul

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/api/option"
)

const rhizaCrashChildEnv = "YEOUL_RHIZA_CRASH_CHILD"
const yeouldTestBinaryEnv = "YEOULD_TEST_BINARY"

func TestRecoveryErrorCausesTraversesJoinedErrors(t *testing.T) {
	backup := &RhizaBackupConfig{SecretKey: "local-test-secret"}
	err := errors.Join(
		&Error{Cause: errors.Join(errors.New("provider rejected local-test-secret"), errors.New("second provider cause"))},
		errors.New("lock release cause"),
	)

	got := recoveryErrorCauses(err, backup)
	for _, want := range []string{"provider rejected [redacted]", "second provider cause", "lock release cause"} {
		if !strings.Contains(got, want) {
			t.Fatalf("recoveryErrorCauses() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, backup.SecretKey) {
		t.Fatalf("recoveryErrorCauses() leaked emulator credentials: %q", got)
	}
}

// This deterministic process boundary verifies acknowledged Yeoul writes can
// be recovered after a process dies without running Rhiza.Close. It is not a
// substitute for distributed or ChaosMesh fault-injection testing.
func TestRhizaAbruptProcessCrashReopensCanonicalState(t *testing.T) {
	if path := os.Getenv(rhizaCrashChildEnv); path != "" {
		rhizaCrashChild(t, path)
		return
	}

	root := t.TempDir()
	dbPath := filepath.Join(root, "memory")
	expectedPath := filepath.Join(root, "expected.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRhizaAbruptProcessCrashReopensCanonicalState$")
	cmd.Env = recoverySubprocessEnv(rhizaCrashChildEnv+"="+dbPath, "YEOUL_RHIZA_EXPECTED="+expectedPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash writer subprocess: %v\n%s", err, output)
	}

	expectedBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read child snapshot: %v", err)
	}
	var expected DatabaseSnapshot
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("decode child snapshot: %v", err)
	}

	ctx := context.Background()
	reopened, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverRhiza})
	if err != nil {
		t.Fatalf("reopen after process exit without Close: %v", err)
	}
	defer func() { _ = reopened.Close(ctx) }()
	assertRecoveryOracle(t, ctx, reopened, &expected)

	if _, err := reopened.IngestEpisode(ctx, EpisodeInput{ID: "ep:after-crash", Kind: "note", Content: "post crash write"}); err != nil {
		t.Fatalf("write after crash recovery: %v", err)
	}
	state, err := Snapshot(ctx, reopened)
	if err != nil {
		t.Fatalf("snapshot after post-recovery write: %v", err)
	}
	if _, ok := state.Episodes["ep:after-crash"]; !ok {
		t.Fatal("post-recovery write is absent from canonical snapshot")
	}
}

func recoverySubprocessEnv(values ...string) []string {
	keys := make(map[string]struct{}, len(values))
	for _, value := range values {
		key, _, _ := strings.Cut(value, "=")
		keys[key] = struct{}{}
	}
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replaced := keys[key]; !replaced {
			env = append(env, value)
		}
	}
	return append(env, values...)
}

// TestRhizaObjectStoreFullLossRecovery runs one provider per invocation. The
// CI harness invokes it separately for S3, GCS, and Azure against isolated,
// pinned local emulators; direct runs without YEOUL_RECOVERY_PROVIDER skip.
func TestRhizaObjectStoreFullLossRecovery(t *testing.T) {
	provider := strings.ToLower(os.Getenv("YEOUL_RECOVERY_PROVIDER"))
	if provider == "" {
		t.Skip("run through scripts/ci/test-rhiza-object-recovery.sh to exercise all providers")
	}
	if provider != "s3" && provider != "gcs" && provider != "azure" {
		t.Fatalf("unsupported YEOUL_RECOVERY_PROVIDER %q", provider)
	}
	backup := rhizaBackupConfigFromEnv(t, provider)
	bootstrapRecoveryBucket(t, backup)
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "source")
	backup.Prefix = recoveryPrefix(backup.Prefix, "recovery-"+recoveryUniqueSuffix())

	source, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: dbPath, CreateIfMissing: true, RhizaBackup: backup})
	if err != nil {
		t.Fatalf("open %s source database: %v; %s", provider, err, recoveryErrorCauses(err, backup))
	}
	entity, episode, oldFact := seedRecoveryOracle(t, ctx, source)
	if _, err := source.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID: "fact:successor", Predicate: oldFact.Predicate, SubjectID: entity.ID,
		ValueText: "comet archive revised", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "verified supersession"); err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	retracted, err := source.AssertFact(ctx, FactInput{
		ID: "fact:retracted", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "obsolete comet note", SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact to retract: %v", err)
	}
	if _, err := source.RetractFact(ctx, retracted.ID, "no longer valid"); err != nil {
		t.Fatalf("retract fact: %v", err)
	}
	if _, err := source.AssertFact(ctx, FactInput{
		ID: "fact:active", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "active comet archive", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert active fact: %v", err)
	}
	expected, err := Snapshot(ctx, source)
	if err != nil {
		t.Fatalf("snapshot before backup completion: %v", err)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatalf("close source and finish pending backup work: %v", err)
	}
	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatalf("remove test-owned source database for total-loss simulation: %v", err)
	}
	if binary := os.Getenv(yeouldTestBinaryEnv); binary != "" {
		configPath := filepath.Join(root, "rhiza-backup.json")
		configData, err := json.Marshal(backup)
		if err != nil {
			t.Fatalf("encode Rhiza backup config for yeould: %v", err)
		}
		if err := os.WriteFile(configPath, configData, 0o600); err != nil {
			t.Fatalf("write test-owned Rhiza backup config: %v", err)
		}
		cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		cmd := exec.CommandContext(cmdCtx, binary, "--db", dbPath, "--rhiza-backup-config", configPath, "--restore-if-missing", "--restore-only")
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("yeould restore-only handoff for %s: %v\n%s", provider, err, output)
		}

		handedOff, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: dbPath, RhizaBackup: backup})
		if err != nil {
			t.Fatalf("open %s database after yeould restore-only handoff without restore: %v", provider, err)
		}
		assertRecoveryOracle(t, ctx, handedOff, expected)
		if err := handedOff.Close(ctx); err != nil {
			t.Fatalf("close database after yeould restore-only handoff: %v", err)
		}
	}

	restored, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: dbPath, RhizaBackup: backup, RestoreIfMissing: true})
	if err != nil {
		t.Fatalf("restore %s database after complete local-directory loss: %v", provider, err)
	}
	assertRecoveryOracle(t, ctx, restored, expected)
	if _, err := restored.IngestEpisode(ctx, EpisodeInput{ID: "ep:after-restore", Kind: "note", Content: "write after remote restore"}); err != nil {
		t.Fatalf("write after restore: %v", err)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close restored database: %v", err)
	}
	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatalf("remove test-owned restored directory for second recovery: %v", err)
	}
	second, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: dbPath, RhizaBackup: backup, RestoreIfMissing: true})
	if err != nil {
		t.Fatalf("restore after post-recovery write: %v", err)
	}
	defer func() { _ = second.Close(ctx) }()
	state, err := Snapshot(ctx, second)
	if err != nil {
		t.Fatalf("snapshot after second restore: %v", err)
	}
	if _, ok := state.Episodes["ep:after-restore"]; !ok {
		t.Fatal("post-recovery write was not present after second total-loss restore")
	}
}

func TestRhizaRestoreIfMissingRejectsMissingRemoteArchive(t *testing.T) {
	provider := strings.ToLower(os.Getenv("YEOUL_RECOVERY_PROVIDER"))
	if provider == "" {
		t.Skip("run through scripts/ci/test-rhiza-object-recovery.sh to exercise all providers")
	}
	backup := rhizaBackupConfigFromEnv(t, provider)
	bootstrapRecoveryBucket(t, backup)
	backup.Prefix = recoveryPrefix(backup.Prefix, "missing-archive-"+t.Name())
	dbPath := filepath.Join(t.TempDir(), "missing")
	_, err := Open(context.Background(), Config{Driver: StorageDriverRhiza, DatabasePath: dbPath, RhizaBackup: backup, RestoreIfMissing: true})
	if err == nil {
		t.Fatal("restore from a missing remote archive initialized an empty database")
	}
	for _, cfg := range []Config{
		{Driver: StorageDriverRhiza, DatabasePath: dbPath},
		{Driver: StorageDriverRhiza, DatabasePath: dbPath, CreateIfMissing: true},
	} {
		if _, reopenErr := Open(context.Background(), cfg); reopenErr == nil {
			t.Fatalf("ordinary open accepted database after failed remote restore (CreateIfMissing=%t)", cfg.CreateIfMissing)
		}
	}
}

func TestRhizaBackupAndRestoreRejectReadOnlyBeforeOpeningPath(t *testing.T) {
	provider := strings.ToLower(os.Getenv("YEOUL_RECOVERY_PROVIDER"))
	if provider == "" {
		t.Skip("run through scripts/ci/test-rhiza-object-recovery.sh to exercise all providers")
	}
	backup := rhizaBackupConfigFromEnv(t, provider)
	for _, cfg := range []Config{
		{Driver: StorageDriverRhiza, DatabasePath: filepath.Join(t.TempDir(), "backup-read-only"), RhizaBackup: backup, ReadOnly: true, CreateIfMissing: true},
		{Driver: StorageDriverRhiza, DatabasePath: filepath.Join(t.TempDir(), "restore-read-only"), RhizaBackup: backup, ReadOnly: true, RestoreIfMissing: true},
	} {
		if _, err := Open(context.Background(), cfg); err == nil {
			t.Fatalf("read-only open accepted backup/restore configuration: %#v", cfg)
		}
		if _, err := os.Lstat(cfg.DatabasePath); !os.IsNotExist(err) {
			t.Fatalf("read-only config touched local path %q (stat error %v)", cfg.DatabasePath, err)
		}
	}
}

func rhizaBackupConfigFromEnv(t *testing.T, provider string) *RhizaBackupConfig {
	t.Helper()
	get := func(name string) string {
		return os.Getenv("YEOUL_RECOVERY_" + strings.ToUpper(provider) + "_" + name)
	}
	endpoint := get("ENDPOINT")
	if err := requireLoopbackHTTP(endpoint); err != nil {
		t.Fatalf("YEOUL_RECOVERY_%s_ENDPOINT must be an explicit loopback HTTP emulator URL: %v", strings.ToUpper(provider), err)
	}
	backup := &RhizaBackupConfig{
		Provider: provider, Bucket: get("BUCKET"), Prefix: get("PREFIX"),
	}
	if backup.Bucket == "" {
		t.Fatalf("YEOUL_RECOVERY_%s_BUCKET is required", strings.ToUpper(provider))
	}
	switch provider {
	case "s3":
		u, _ := url.Parse(endpoint)
		backup.Endpoint = u.Host
		backup.Region = "us-east-1"
		backup.Insecure = true
		backup.AccessKey = "yeoul-local-test-access"
		backup.SecretKey = "yeoul-local-test-secret"
	case "gcs":
		if get("SERVICE_ACCOUNT") != "" {
			t.Fatal("GCS recovery tests must use unauthenticated loopback emulator mode, not service-account credentials")
		}
		backup.Endpoint = ""
		t.Setenv("STORAGE_EMULATOR_HOST", endpoint)
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
		t.Setenv("GOOGLE_CLOUD_PROJECT", "yeoul-local-test")
	case "azure":
		backup.Endpoint = endpoint
		account := "yeoul-local-test"
		key := base64.StdEncoding.EncodeToString([]byte("yeoul-local-test-account-key"))
		backup.AzureStorageAccount = account
		backup.AzureConnectionString = fmt.Sprintf("DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=%s;BlobEndpoint=%s/%s;", account, key, strings.TrimSuffix(endpoint, "/"), account)
		if err := requireLoopbackAzureConnectionString(backup.AzureConnectionString, endpoint); err != nil {
			t.Fatalf("constructed Azure emulator connection string is not loopback-only: %v", err)
		}
	}
	return backup
}

func bootstrapRecoveryBucket(t *testing.T, backup *RhizaBackupConfig) {
	t.Helper()
	ctx := context.Background()
	switch backup.Provider {
	case "s3":
		client, err := minio.New(backup.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(backup.AccessKey, backup.SecretKey, ""),
			Secure: false, Region: backup.Region, BucketLookup: minio.BucketLookupPath,
		})
		if err != nil {
			t.Fatalf("create S3 emulator client: %v", err)
		}
		exists, err := client.BucketExists(ctx, backup.Bucket)
		if err != nil {
			t.Fatalf("check S3 emulator test bucket: %v", err)
		}
		if !exists {
			if err := client.MakeBucket(ctx, backup.Bucket, minio.MakeBucketOptions{Region: backup.Region}); err != nil {
				t.Fatalf("create S3 emulator test bucket: %v", err)
			}
		}
	case "gcs":
		client, err := storage.NewClient(ctx, option.WithEndpoint(os.Getenv("STORAGE_EMULATOR_HOST")), option.WithoutAuthentication())
		if err != nil {
			t.Fatalf("create GCS emulator client: %v", err)
		}
		defer func() { _ = client.Close() }()
		bucket := client.Bucket(backup.Bucket)
		if _, err := bucket.Attrs(ctx); err == nil {
			return
		}
		if err := bucket.Create(ctx, "yeoul-local-test", nil); err != nil {
			if _, verifyErr := bucket.Attrs(ctx); verifyErr != nil {
				t.Fatalf("create or verify GCS emulator test bucket: %v", errors.Join(err, verifyErr))
			}
		}
	case "azure":
		client, err := azblob.NewClientFromConnectionString(backup.AzureConnectionString, nil)
		if err != nil {
			t.Fatalf("create Azure emulator client: %v", err)
		}
		if _, err := client.CreateContainer(ctx, backup.Bucket, nil); err != nil {
			_, verifyErr := client.ServiceClient().NewContainerClient(backup.Bucket).GetProperties(ctx, nil)
			if verifyErr != nil {
				t.Fatalf("create or verify Azure emulator test container: %v", errors.Join(err, verifyErr))
			}
		}
	default:
		t.Fatalf("unsupported recovery test provider %q", backup.Provider)
	}
}

func requireLoopbackHTTP(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" || u.User != nil || u.Hostname() == "" || u.Port() == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("expected http URL with explicit host and port, no credentials, query, or fragment")
	}
	if !strings.EqualFold(u.Hostname(), "localhost") {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("host %q is not localhost or a loopback IP", u.Hostname())
		}
	}
	return nil
}

func requireLoopbackAzureConnectionString(connectionString, endpoint string) error {
	values := make(map[string]string)
	for _, part := range strings.Split(connectionString, ";") {
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return fmt.Errorf("invalid connection-string component")
		}
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	blobEndpoint, ok := values["blobendpoint"]
	if !ok {
		return fmt.Errorf("BlobEndpoint is required")
	}
	for key, value := range values {
		if strings.HasSuffix(key, "endpoint") {
			if err := requireLoopbackHTTP(value); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	blobURL, _ := url.Parse(blobEndpoint)
	baseURL, _ := url.Parse(endpoint)
	if !strings.EqualFold(blobURL.Host, baseURL.Host) || blobURL.Scheme != baseURL.Scheme {
		return fmt.Errorf("BlobEndpoint must use the explicitly configured local endpoint")
	}
	if values["accountname"] != "yeoul-local-test" || values["accountkey"] != base64.StdEncoding.EncodeToString([]byte("yeoul-local-test-account-key")) {
		return fmt.Errorf("Azure emulator credentials must use fixed local test values")
	}
	return nil
}

func recoveryPrefix(base, suffix string) string {
	base = strings.Trim(base, "/")
	if base == "" {
		return suffix
	}
	return base + "/" + suffix
}

func recoveryErrorCauses(err error, backup *RhizaBackupConfig) string {
	var chain []string
	var walk func(error, int)
	walk = func(current error, depth int) {
		if current == nil || depth > 64 {
			return
		}
		if public, ok := current.(*Error); ok && public.Cause != nil {
			walk(public.Cause, depth+1)
			return
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, cause := range joined.Unwrap() {
				walk(cause, depth+1)
			}
			return
		}
		if cause := errors.Unwrap(current); cause != nil {
			walk(cause, depth+1)
			return
		}
		message := current.Error()
		for _, secret := range []string{backup.AccessKey, backup.SecretKey, backup.AzureStorageAccountKey, backup.AzureConnectionString} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		chain = append(chain, fmt.Sprintf("%T: %s", current, message))
	}
	walk(err, 0)
	if len(chain) == 0 {
		return "cause chain: <none>"
	}
	return "cause chain: " + strings.Join(chain, " <- ")
}

func rhizaCrashChild(t *testing.T, dbPath string) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverRhiza, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open crash writer: %v", err)
	}
	entity, episode, oldFact := seedRecoveryOracle(t, ctx, eng)
	if _, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID: "fact:successor", Predicate: oldFact.Predicate, SubjectID: entity.ID,
		ValueText: "comet archive revised", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "verified supersession"); err != nil {
		t.Fatalf("supersede fact: %v", err)
	}
	retracted, err := eng.AssertFact(ctx, FactInput{
		ID: "fact:retracted", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "obsolete comet note", SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact to retract: %v", err)
	}
	if _, err := eng.RetractFact(ctx, retracted.ID, "no longer valid"); err != nil {
		t.Fatalf("retract fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID: "fact:active", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "active comet archive", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert active fact: %v", err)
	}

	snapshot, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot before process exit: %v", err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode expected snapshot: %v", err)
	}
	if err := os.WriteFile(os.Getenv("YEOUL_RHIZA_EXPECTED"), data, 0o600); err != nil {
		t.Fatalf("persist expected snapshot: %v", err)
	}
	// Deliberately bypass Engine.Close to model abrupt process termination.
	os.Exit(0)
}

func seedRecoveryOracle(t *testing.T, ctx context.Context, eng Engine) (*Entity, *EpisodeResult, *Fact) {
	t.Helper()
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID: "ep:recovery-oracle", Kind: "note",
		Content: "comet archive recovery keyword observatory record",
		Source:  SourceInput{Kind: "note", ExternalRef: "recovery-oracle"},
	})
	if err != nil {
		t.Fatalf("ingest oracle episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID: "entity:recovery-oracle", Type: "Project", CanonicalName: "Comet Archive",
		Aliases: []string{"Observatory"}, Metadata: map[string]any{"tier": "initial"},
	})
	if err != nil {
		t.Fatalf("upsert oracle entity: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{
		ID: entity.ID, Type: "Project", CanonicalName: "Comet Archive Verified",
		Aliases: []string{"Observatory", "Archive"}, Metadata: map[string]any{"tier": "verified"},
	}); err != nil {
		t.Fatalf("revise oracle entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		ID: "fact:superseded", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "original comet archive", SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert original fact: %v", err)
	}
	return entity, episode, fact
}

func assertRecoveryOracle(t *testing.T, ctx context.Context, eng Engine, expected *DatabaseSnapshot) {
	t.Helper()
	got, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot recovered database: %v", err)
	}
	wantBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected recovery state: %v", err)
	}
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal recovered state: %v", err)
	}
	if !bytes.Equal(wantBytes, gotBytes) {
		t.Fatalf("canonical Yeoul state differs after recovery: want=%s; got=%s", wantBytes, gotBytes)
	}
	wantHash, gotHash := sha256.Sum256(wantBytes), sha256.Sum256(gotBytes)
	if wantHash != gotHash {
		t.Fatalf("canonical recovery state checksum differs: want %x, got %x", wantHash, gotHash)
	}
	if got.Episodes["ep:recovery-oracle"].Content != "comet archive recovery keyword observatory record" || got.Entities["entity:recovery-oracle"].CanonicalName != "Comet Archive Verified" {
		t.Fatal("recovery sentinels are missing; an empty or partial initialization may have been opened")
	}
	if len(got.FactRevisions) < 4 || len(got.EntityRevisions) < 2 {
		t.Fatalf("lifecycle revisions missing: facts=%d entities=%d", len(got.FactRevisions), len(got.EntityRevisions))
	}
	if got.Facts["fact:superseded"].Status != factStatusSuperseded || got.Facts["fact:retracted"].Status != factStatusRetracted || got.Facts["fact:active"].Status != factStatusActive {
		t.Fatalf("fact lifecycle states were not preserved: superseded=%q retracted=%q active=%q", got.Facts["fact:superseded"].Status, got.Facts["fact:retracted"].Status, got.Facts["fact:active"].Status)
	}

	search, err := eng.Search(ctx, SearchRequest{QueryText: "recovery keyword observatory", Mode: SearchModeFullText, Types: []string{"episode", "fact", "entity"}})
	if err != nil {
		t.Fatalf("full-text search after recovery: %v", err)
	}
	if !recoverySearchHas(search.Hits, "ep:recovery-oracle") {
		t.Fatalf("full-text search did not find recovery sentinel episode: %#v", search.Hits)
	}

	oldFact := got.Facts["fact:superseded"]
	cut := oldFact.CreatedAt
	historical, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{oldFact.SubjectID}, Temporal: TemporalFilter{AsOf: &cut, IncludeInactive: true}})
	if err != nil {
		t.Fatalf("historical fact lookup after recovery: %v", err)
	}
	if !recoveryFactHas(historical.Facts, oldFact.ID) || recoveryFactHas(historical.Facts, "fact:successor") {
		t.Fatalf("as_of lookup lost historical state: %#v", historical.Facts)
	}
	timeline, err := eng.Timeline(ctx, TimelineRequest{AnchorIDs: []string{oldFact.SubjectID}, Temporal: TemporalFilter{AsOf: &cut}})
	if err != nil {
		t.Fatalf("historical timeline after recovery: %v", err)
	}
	if !recoveryTimelineHas(timeline.Events, "evt:"+oldFact.ID+":created") || recoveryTimelineHas(timeline.Events, "evt:fact:successor:created") {
		t.Fatalf("as_of timeline lost historical boundary: %#v", timeline.Events)
	}
	provenance, err := eng.Provenance(ctx, ProvenanceRequest{Kind: "fact", ID: "fact:successor"})
	if err != nil {
		t.Fatalf("provenance after recovery: %v", err)
	}
	if !recoveryProvenanceHas(provenance.Nodes, "ep:recovery-oracle") {
		t.Fatalf("provenance lost supporting episode: %#v", provenance.Nodes)
	}
}

func recoverySearchHas(hits []SearchHit, id string) bool {
	for _, hit := range hits {
		if hit.RecordID == id {
			return true
		}
	}
	return false
}

func recoveryFactHas(facts []Fact, id string) bool {
	for _, fact := range facts {
		if fact.ID == id {
			return true
		}
	}
	return false
}

func recoveryTimelineHas(events []TimelineEvent, id string) bool {
	for _, event := range events {
		if event.EventID == id {
			return true
		}
	}
	return false
}

func recoveryProvenanceHas(nodes []ProvenanceNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}
