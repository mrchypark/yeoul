package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := verifyBuild(); err != nil {
		return err
	}
	dbPath := os.Getenv("YEOUL_LEGACY_DB_PATH")
	if err := validateFreshTarget(dbPath, os.Getenv("YEOUL_LEGACY_ROOT"), os.Getenv("YEOUL_LEGACY_OWNER_TOKEN")); err != nil {
		return err
	}
	backup, err := backupConfig()
	if err != nil {
		return err
	}
	ctx := context.Background()
	engine, err := yeoul.Open(ctx, yeoul.Config{
		Driver: yeoul.StorageDriverRhiza, DatabasePath: dbPath,
		CreateIfMissing: true, RhizaBackup: backup,
	})
	if err != nil {
		return fmt.Errorf("open v0.12.0 Yeoul database: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = engine.Close(ctx)
		}
	}()
	episode, err := engine.IngestEpisode(ctx, yeoul.EpisodeInput{
		ID: "ep:legacy-v020", Kind: "note", Content: "legacy v0.20 Rhiza archive sentinel",
	})
	if err != nil {
		return fmt.Errorf("write legacy episode: %w", err)
	}
	entity, err := engine.UpsertEntity(ctx, yeoul.EntityInput{
		ID: "entity:legacy-v020", Type: "Project", CanonicalName: "Legacy Rhiza Fixture",
	})
	if err != nil {
		return fmt.Errorf("write legacy entity: %w", err)
	}
	if _, err := engine.AssertFact(ctx, yeoul.FactInput{
		ID: "fact:legacy-v020", Predicate: "FIXTURE_VERSION", SubjectID: entity.ID,
		ValueText:            "Yeoul v0.12.0 with Rhiza v0.20.0",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		return fmt.Errorf("write legacy fact: %w", err)
	}
	if err := engine.Close(ctx); err != nil {
		return fmt.Errorf("close legacy fixture and complete BeforeAck writes: %w", err)
	}
	closed = true
	fmt.Println("fixture generated with github.com/mrchypark/yeoul v0.12.0 and github.com/mrchypark/rhiza v0.20.0 (no replacements)")
	return nil
}

func validateFreshTarget(dbPath, ownerRoot, ownerToken string) error {
	if !filepath.IsAbs(dbPath) || !filepath.IsAbs(ownerRoot) || ownerToken == "" {
		return errors.New("legacy fixture requires absolute database/root paths and an owner token")
	}
	root, err := filepath.EvalSymlinks(ownerRoot)
	if err != nil {
		return fmt.Errorf("resolve test-owned fixture root: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	dbPath, err = filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	dbParent, err := filepath.EvalSymlinks(filepath.Dir(dbPath))
	if err != nil {
		return fmt.Errorf("resolve legacy database parent: %w", err)
	}
	dbParent, err = filepath.Abs(dbParent)
	if err != nil {
		return err
	}
	if dbParent != root {
		return errors.New("legacy database path must be a direct child of the test-owned fixture root")
	}
	marker := filepath.Join(root, ".yeoul-legacy-fixture-owner")
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("test-owned fixture marker missing or invalid: %v", err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != ownerToken {
		return errors.New("test-owned fixture marker does not match owner token")
	}
	if _, err := os.Lstat(dbPath); err == nil {
		return fmt.Errorf("refusing to open legacy fixture source path because it already exists: %s", dbPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect legacy fixture source path: %w", err)
	}
	return nil
}

func verifyBuild() error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("runtime build information is unavailable")
	}
	want := map[string]string{
		"github.com/mrchypark/yeoul": "v0.12.0",
		"github.com/mrchypark/rhiza": "v0.20.0",
	}
	for _, dep := range info.Deps {
		if version, exists := want[dep.Path]; exists {
			if dep.Version != version || dep.Replace != nil {
				return fmt.Errorf("unexpected %s build selection: version=%s replace=%v", dep.Path, dep.Version, dep.Replace)
			}
			delete(want, dep.Path)
		}
	}
	if len(want) != 0 {
		return fmt.Errorf("runtime build is missing published dependencies: %v", want)
	}
	return nil
}

func backupConfig() (*yeoul.RhizaBackupConfig, error) {
	get := func(key string) string { return os.Getenv("YEOUL_LEGACY_" + key) }
	provider, endpoint := strings.ToLower(get("PROVIDER")), get("ENDPOINT")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("YEOUL_LEGACY_ENDPOINT must be an explicit loopback HTTP URL")
	}
	ip := net.ParseIP(parsed.Hostname())
	if !strings.EqualFold(parsed.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("YEOUL_LEGACY_ENDPOINT must use a loopback host")
	}
	bucket, prefix := get("BUCKET"), get("PREFIX")
	if bucket == "" || prefix == "" {
		return nil, errors.New("YEOUL_LEGACY_BUCKET and YEOUL_LEGACY_PREFIX are required")
	}
	cfg := &yeoul.RhizaBackupConfig{Provider: provider, Bucket: bucket, Prefix: prefix}
	switch provider {
	case "s3":
		cfg.Endpoint = parsed.Host
		cfg.Region, cfg.Insecure = "us-east-1", true
		cfg.AccessKey, cfg.SecretKey = "yeoul-local-test-access", "yeoul-local-test-secret"
	case "gcs":
		if os.Getenv("STORAGE_EMULATOR_HOST") != endpoint || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
			return nil, errors.New("GCS legacy fixture requires the explicit unauthenticated Floci emulator environment")
		}
	case "azure":
		account := "yeoul-local-test"
		cfg.AzureStorageAccount = account
		cfg.AzureConnectionString = "DefaultEndpointsProtocol=http;AccountName=" + account +
			";AccountKey=eWVvdWwtbG9jYWwtdGVzdC1hY2NvdW50LWtleQ==;BlobEndpoint=" + strings.TrimSuffix(endpoint, "/") + "/" + account + ";"
	default:
		return nil, fmt.Errorf("unsupported legacy fixture provider %q", provider)
	}
	return cfg, nil
}
